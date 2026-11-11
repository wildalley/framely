// Package components locates optional, platform-specific native workers.
package components

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Find searches explicit or installed resource directories. A working directory
// (including one with a forged go.mod) is never an executable search root.
// Workers are launched directly, never through a shell or a PATH lookup.
func Find(name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("组件名称无效")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Dir(exe)
	var roots []string
	override := os.Getenv("FRAMELY_COMPONENTS_DIR")
	if override == "" {
		override = os.Getenv("SCREENPIN_COMPONENTS_DIR") // compatibility for development scripts
	}
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("FRAMELY_COMPONENTS_DIR 必须是绝对路径")
		}
		// Explicit configuration is authoritative: missing or stale development
		// components must not silently fall back to another installed runtime.
		return findIn(name, []string{override})
	}
	roots = append(roots, filepath.Join(base, "workers"))
	if runtime.GOOS == "darwin" {
		roots = append(roots, filepath.Join(base, "..", "Resources", "workers"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".local", "share", "framely", "workers"))
	}
	return findIn(name, roots)
}

func findIn(name string, roots []string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, root := range roots {
		for _, path := range []string{filepath.Join(root, name), filepath.Join(root, "framely-ocr", name)} {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return filepath.Abs(path)
			}
		}
	}
	return "", fmt.Errorf("缺少 %s 组件，请安装包含 OCR/长截图组件的完整原生包", name)
}
