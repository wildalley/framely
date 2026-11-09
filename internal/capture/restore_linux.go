//go:build linux

package capture

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"
)

var restoreMu sync.Mutex

func restorePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "screenpin", "scroll-source.json"), nil
}
func restoreTokens(path string) map[string]string {
	tokens := make(map[string]string)
	data, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(data, &tokens) != nil {
			return make(map[string]string)
		}
	}
	if tokens == nil {
		tokens = make(map[string]string)
	}
	return tokens
}
func writeRestoreTokens(path string, tokens map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".scroll-source-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Tokens are single-use. Consume before sending, and store only the portal's
// newly granted token; don't log tokens or mix pointer and screen grants.
func takeRestoreToken(iface string) string {
	token, _ := takeDisplayRestoreToken(iface, image.Rectangle{})
	return token
}

func displayRestoreKey(iface string, bounds image.Rectangle) string {
	if bounds.Empty() {
		return iface
	}
	return fmt.Sprintf("%s/display/%d,%d,%d,%d", iface, bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy())
}

// Prefer this display's token; a legacy unscoped token is validated against
// the returned monitor before capture. Never borrow another display's grant.
func takeDisplayRestoreToken(iface string, bounds image.Rectangle) (string, string) {
	restoreMu.Lock()
	defer restoreMu.Unlock()
	path, err := restorePath()
	if err != nil {
		return "", ""
	}
	tokens := restoreTokens(path)
	key := displayRestoreKey(iface, bounds)
	if tokens[key] == "" {
		key = iface
	}
	token := tokens[key]
	if token != "" {
		delete(tokens, key)
		if writeRestoreTokens(path, tokens) != nil {
			return "", ""
		}
	}
	return token, key
}
func saveRestoreToken(iface, token string) error {
	return saveDisplayRestoreToken(iface, token, image.Rectangle{})
}

func saveDisplayRestoreToken(iface, token string, bounds image.Rectangle) error {
	restoreMu.Lock()
	defer restoreMu.Unlock()
	path, err := restorePath()
	if err != nil {
		return err
	}
	tokens := restoreTokens(path)
	key := displayRestoreKey(iface, bounds)
	if token == "" {
		delete(tokens, key)
	} else {
		for oldKey, oldToken := range tokens {
			if oldToken == token {
				delete(tokens, oldKey)
			}
		}
		tokens[key] = token
	}
	return writeRestoreTokens(path, tokens)
}
func ForgetScrollSource() error {
	restoreMu.Lock()
	defer restoreMu.Unlock()
	path, err := restorePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
