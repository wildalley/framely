// Package apppaths owns Framely's persistent paths and one-time migration.
package apppaths

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var migrationMu sync.Mutex

// ConfigFile migrates the old directory before returning the canonical path.
// Keep the portal ID separate from these user-visible filesystem names.
func ConfigFile(name string) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir, err := configDir(root, runtime.GOOS)
	return filepath.Join(dir, name), err
}

func configDir(root, goos string) (string, error) {
	migrationMu.Lock()
	defer migrationMu.Unlock()
	name := "framely"
	if goos == "windows" || goos == "darwin" {
		name = "Framely"
	}
	dir := filepath.Join(root, name)
	for _, legacy := range []string{"screenpin", "ScreenPin"} {
		old := filepath.Join(root, legacy)
		info, err := os.Lstat(old)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", errors.New("legacy configuration is not a regular directory: " + old)
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(old, dir); err != nil {
				return "", err
			}
			continue
		} else if err != nil {
			return "", err
		}
		// Keep an existing Framely configuration authoritative. Copy missing
		// known files, then retire the entire old directory into a backup so
		// consumed/deleted portal tokens can never be imported a second time.
		for _, file := range []string{"config.json", "scroll-source.json", "recording.json"} {
			if err := copyMissing(filepath.Join(old, file), filepath.Join(dir, file)); err != nil {
				return "", err
			}
		}
		backup, err := os.MkdirTemp(dir, "legacy-config-backup-")
		if err != nil {
			return "", err
		}
		if err := os.Rename(old, filepath.Join(backup, "config")); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func copyMissing(source, target string) error {
	input, err := os.Open(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	err = errors.Join(copyErr, output.Close())
	if err != nil {
		os.Remove(target)
	}
	return err
}
