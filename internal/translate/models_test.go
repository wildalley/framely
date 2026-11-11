package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
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
