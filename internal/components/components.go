// Package components locates optional, platform-specific native workers.
package components

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Find searches the installed resource directory, then the development bundle.
// Workers are launched directly, never through a shell or a PATH lookup.
func Find(name string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Dir(exe)
	var roots []string
	if override := os.Getenv("SCREENPIN_COMPONENTS_DIR"); override != "" {
		roots = append(roots, override)
	}
	roots = append(roots, filepath.Join(base, "workers"))
	if runtime.GOOS == "darwin" {
		roots = append(roots, filepath.Join(base, "..", "Resources", "workers"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".local", "share", "screenpin", "workers"))
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
				if strings.HasPrefix(string(data), "module github.com/wildalley/screenpin\n") {
					roots = append(roots, filepath.Join(dir, "resources", runtime.GOOS+"-"+runtime.GOARCH, "workers"))
				}
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, root := range roots {
		for _, path := range []string{filepath.Join(root, name), filepath.Join(root, "screenpin-ocr", name)} {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return filepath.Abs(path)
			}
		}
	}
	return "", fmt.Errorf("缺少 %s 组件，请安装包含 OCR/长截图组件的完整原生包", name)
}
