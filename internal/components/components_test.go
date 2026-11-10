package components

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkingDirectoryCannotInjectAWorker(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(ending, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("SCREENPIN_COMPONENTS_DIR", "")
			if err := os.WriteFile("go.mod", []byte("module github.com/wildalley/screenpin"+ending+ending+"go 1.27.3"+ending), 0600); err != nil {
				t.Fatal(err)
			}
			name := "screenpin-test-component"
			file := name
			if runtime.GOOS == "windows" {
				file += ".exe"
			}
			worker := filepath.Join(root, "resources", runtime.GOOS+"-"+runtime.GOARCH, "workers", file)
			if err := os.MkdirAll(filepath.Dir(worker), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(worker, nil, 0700); err != nil {
				t.Fatal(err)
			}
			if got, err := Find(name); err == nil {
				t.Fatalf("loaded untrusted cwd component: %q", got)
			}
			t.Setenv("SCREENPIN_COMPONENTS_DIR", filepath.Dir(worker))
			if got, err := Find(name); err != nil || got != worker {
				t.Fatalf("Find() = %q, %v; want %q", got, err, worker)
			}
		})
	}
}

func TestExplicitRootAndNamesAreValidated(t *testing.T) {
	for _, root := range []string{".", "relative/workers"} {
		t.Setenv("SCREENPIN_COMPONENTS_DIR", root)
		if _, err := Find("screenpin-ocr"); err == nil {
			t.Fatal("relative override accepted")
		}
	}
	t.Setenv("SCREENPIN_COMPONENTS_DIR", t.TempDir())
	if _, err := Find("screenpin-ocr"); err == nil {
		t.Fatal("missing explicit worker fell back to installed OCR")
	}
	for _, name := range []string{"", ".", "..", "../screenpin-ocr", `..\screenpin-ocr`, "/screenpin-ocr"} {
		if _, err := Find(name); err == nil {
			t.Fatalf("unsafe name %q", name)
		}
	}
}
