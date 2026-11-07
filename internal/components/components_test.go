package components

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDevelopmentLookupWithCheckoutLineEndings(t *testing.T) {
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
			if got, err := Find(name); err != nil || got != worker {
				t.Fatalf("Find() = %q, %v; want %q", got, err, worker)
			}
		})
	}
}
