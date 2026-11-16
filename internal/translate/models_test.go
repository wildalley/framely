package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pinned(name, content string) ModelFile {
	sum := sha256.Sum256([]byte(content))
	return ModelFile{Name: name, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
}

// fakeModels serves a two-file model and counts the requests.
func fakeModels(t *testing.T, files map[string]string) (Model, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	saved := ModelBaseURL
	ModelBaseURL = srv.URL + "/"
	t.Cleanup(func() { ModelBaseURL = saved })
	return Model{Direction: "en-zh", Files: []ModelFile{pinned("a.onnx", "encoder"), pinned("NOTICE.txt", "notice")}}, &hits
}

func TestDownloadInstallsOnlyVerifiedFiles(t *testing.T) {
	m, hits := fakeModels(t, map[string]string{"en-zh-a.onnx": "encoder", "en-zh-NOTICE.txt": "notice"})
	root := t.TempDir()
	var last int64
	if err := Download(context.Background(), nil, root, m, func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if !Installed(root, m) || last != m.Size() {
		t.Fatalf("installed=%v progress=%d/%d", Installed(root, m), last, m.Size())
	}
	// Files already in place are not fetched again.
	before := hits.Load()
	if err := Download(context.Background(), nil, root, m, nil); err != nil || hits.Load() != before {
		t.Fatalf("redownloaded: %v hits %d→%d", err, before, hits.Load())
	}
	if err := Remove(root, m); err != nil || Installed(root, m) {
		t.Fatalf("remove: %v", err)
	}
	if err := Remove(root, Model{Direction: ".."}); err == nil {
		t.Fatal("removed a path outside the models")
	}
}

func TestTamperedOrOversizedFilesAreDiscarded(t *testing.T) {
	for name, served := range map[string]string{"tampered": "encodeX", "oversized": "encoder plus", "missing": ""} {
		files := map[string]string{"en-zh-NOTICE.txt": "notice"}
		if name != "missing" {
			files["en-zh-a.onnx"] = served
		}
		m, _ := fakeModels(t, files)
		root := t.TempDir()
		if err := Download(context.Background(), nil, root, m, nil); err == nil {
			t.Fatalf("%s: a bad file was accepted", name)
		}
		left, _ := filepath.Glob(filepath.Join(root, "en-zh", "*"))
		if Installed(root, m) || len(left) != 0 {
			t.Fatalf("%s: left %v", name, left)
		}
	}
}

func TestCancelledDownloadLeavesNothingInstalled(t *testing.T) {
	m, _ := fakeModels(t, map[string]string{"en-zh-a.onnx": "encoder", "en-zh-NOTICE.txt": "notice"})
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Download(ctx, nil, root, m, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled download: %v", err)
	}
	if Installed(root, m) {
		t.Fatal("a cancelled download looks installed")
	}
	if _, err := os.Stat(filepath.Join(root, "en-zh", "a.onnx.part")); err == nil {
		t.Fatal("a partial file was left")
	}
}

func TestOfflineTranslatesLineByLineIntoTheRightDirection(t *testing.T) {
	var got string
	o := Options{Provider: Offline, Offline: func(_ context.Context, direction string, lines []string) ([]string, error) {
		got = direction
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = "<" + line + ">"
		}
		return out, nil
	}}
	r, err := Translate(context.Background(), o, "Hello\n\nWorld")
	if err != nil || got != "en-zh" || r.Text != "<Hello>\n<>\n<World>" {
		t.Fatal(r, got, err)
	}
	if _, err := Translate(context.Background(), o, "你好世界"); err != nil || got != "zh-en" {
		t.Fatal(got, err)
	}
	if _, err := Translate(context.Background(), Options{Provider: Offline}, "Hello"); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestPublishedModelsArePinned(t *testing.T) {
	for _, m := range Models {
		names := map[string]bool{}
		for _, f := range m.Files {
			names[f.Name] = true
			if f.Size <= 0 || len(f.SHA256) != 64 {
				t.Fatalf("%s/%s is not pinned", m.Direction, f.Name)
			}
		}
		for _, need := range []string{"encoder_model_quantized.onnx", "decoder_model_quantized.onnx", "source.spm", "target.spm", "vocab.json", "NOTICE.txt"} {
			if !names[need] {
				t.Fatalf("%s lacks %s", m.Direction, need)
			}
		}
	}
}

type modelTestTransport func(*http.Request) (*http.Response, error)

func (f modelTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type interruptedModelBody struct {
	first   bool
	release <-chan struct{}
	failed  bool
}

func (b *interruptedModelBody) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true
		return copy(p, "enc"), nil
	}
	<-b.release
	if b.failed {
		return 0, context.Canceled
	}
	return copy(p, "oder"), io.EOF
}

func (*interruptedModelBody) Close() error { return nil }

func modelTestClient(body io.ReadCloser, started func()) *http.Client {
	return &http.Client{Transport: modelTestTransport(func(*http.Request) (*http.Response, error) {
		if started != nil {
			started()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})}
}

func TestCancelledFetchDoesNotRemoveAnotherAttemptsStagingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.onnx")
	f := pinned("model.onnx", "encoder")
	releaseOld, releaseNew := make(chan struct{}), make(chan struct{})
	startedOld, startedNew := make(chan struct{}, 1), make(chan struct{}, 1)
	oldDone, newDone := make(chan error, 1), make(chan error, 1)
	go func() {
		oldDone <- fetch(context.Background(), modelTestClient(&interruptedModelBody{release: releaseOld, failed: true}, nil), "https://example.invalid/model", path, f, func(int64) {
			select {
			case startedOld <- struct{}{}:
			default:
			}
		})
	}()
	<-startedOld
	go func() {
		newDone <- fetch(context.Background(), modelTestClient(&interruptedModelBody{release: releaseNew}, nil), "https://example.invalid/model", path, f, func(int64) {
			select {
			case startedNew <- struct{}{}:
			default:
			}
		})
	}()
	<-startedNew
	close(releaseOld)
	if err := <-oldDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(releaseNew)
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "encoder" {
		t.Fatalf("retry corrupted: %q %v", data, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("left staging files: %v %v", files, err)
	}
}

func TestQueuedModelDownloadCanBeCancelledAndRetryWaitsForCleanup(t *testing.T) {
	root := t.TempDir()
	m := Model{Direction: "en-zh", Files: []ModelFile{pinned("a.onnx", "encoder")}}
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- Download(context.Background(), modelTestClient(&interruptedModelBody{release: release, failed: true}, nil), root, m, func(done, total int64) {
			select {
			case started <- struct{}{}:
			default:
			}
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() { waitDone <- Download(ctx, nil, root, m, nil) }()
	select {
	case err := <-waitDone:
		t.Fatalf("queued download returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-waitDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation waited for active download")
	}
	retryStarted := make(chan struct{}, 1)
	retryDone := make(chan error, 1)
	go func() {
		retryDone <- Download(context.Background(), modelTestClient(io.NopCloser(strings.NewReader("encoder")), func() { retryStarted <- struct{}{} }), root, m, nil)
	}()
	select {
	case <-retryStarted:
		t.Fatal("retry overlapped active download")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-oldDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := <-retryDone; err != nil {
		t.Fatal(err)
	}
	if !Installed(root, m) {
		t.Fatal("retry not installed")
	}
	key, _ := filepath.Abs(filepath.Join(root, m.Direction))
	modelOperations.mu.Lock()
	left := modelOperations.active[key]
	modelOperations.mu.Unlock()
	if left != nil {
		t.Fatal("unused model operation retained")
	}
}

func TestQueuedModelRemovalCanBeCancelled(t *testing.T) {
	root := t.TempDir()
	m := Model{Direction: "en-zh"}
	dir := filepath.Join(root, m.Direction)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockModel(context.Background(), root, m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RemoveContext(ctx, root, m) }()
	select {
	case err := <-done:
		t.Fatalf("removal did not wait for the active download: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("removal cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled removal waited for the active download")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("cancelled removal deleted the model: %v", err)
	}
	unlock()
	unlock = nil
	if err := RemoveContext(context.Background(), root, m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("model not removed: %v", err)
	}
}
