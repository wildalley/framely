package ocr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// translateTestWorker stands in for the worker's --translate mode: it
// echoes each line tagged with the direction, or reports an error.
func translateTestWorker() {
	var request struct{ Lines []string }
	_ = json.NewDecoder(os.Stdin).Decode(&request)
	args := os.Args[1:]
	if len(args) != 3 || args[0] != "--translate" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": "bad arguments"})
		return
	}
	if args[2] == "xx-yy" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": "不支持的翻译方向"})
		return
	}
	out := make([]string, len(request.Lines))
	for i, line := range request.Lines {
		out[i] = args[2] + ":" + filepath.Base(args[1]) + ":" + strings.ToUpper(line)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string][]string{"lines": out})
}

func TestTranslateRunsTheWorkersTranslateMode(t *testing.T) {
	installTestWorker(t)
	t.Setenv("SCREENPIN_OCR_HELPER_TEST", "translate")
	dir := os.Getenv("SCREENPIN_COMPONENTS_DIR")
	manifest := filepath.Join(dir, "manifest.json")
	// A worker built before offline translation is refused with a reason.
	if err := os.WriteFile(manifest, []byte(`{"protocol":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Translate(context.Background(), "/models/en-zh", "en-zh", []string{"a"}); err == nil || !strings.Contains(err.Error(), "新版") {
		t.Fatalf("an old worker: %v", err)
	}
	if err := os.WriteFile(manifest, []byte(`{"protocol":2,"translate":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Translate(context.Background(), "/models/en-zh", "en-zh", []string{"hello", "", "中文"})
	if err != nil || strings.Join(got, "|") != "en-zh:en-zh:HELLO|en-zh:en-zh:|en-zh:en-zh:中文" {
		t.Fatal(got, err)
	}
	if _, err := Translate(context.Background(), "/models/xx", "xx-yy", []string{"a"}); err == nil || !strings.Contains(err.Error(), "不支持的翻译方向") {
		t.Fatalf("worker error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Translate(ctx, "/models/en-zh", "en-zh", []string{"a"}); err == nil {
		t.Fatal("a cancelled translation succeeded")
	}
}
