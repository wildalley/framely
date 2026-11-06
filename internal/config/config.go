// Package config keeps ScreenPin's settings in config.json, in the place
// and format the previous version used, so they carry over.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wildalley/screenpin/internal/shortcutkey"
)

// DefaultShortcut is the global capture shortcut until the user records
// another: Win+Shift+S, as on Windows.
const DefaultShortcut = "Super+Shift+S"

// Config is the user's settings.
type Config struct {
	CaptureShortcut string `json:"captureShortcut"`
	// HideAfterCopy hides the editor to the tray once the image is copied.
	HideAfterCopy bool `json:"hideAfterCopy"`
}

// Default is the configuration of a first launch.
func Default() Config { return Config{CaptureShortcut: DefaultShortcut} }

// Path is where the configuration is kept: ~/.config/screenpin/config.json
// on Linux, and the platform's configuration directory elsewhere.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "screenpin", "config.json"), nil
}

// Load reads the configuration, or the default one when there is none or it
// cannot be read. A saved shortcut that is not valid falls back to the
// default, and repaired reports it so the caller can save the fix.
func Load() (c Config, repaired bool) {
	c = Default()
	path, err := Path()
	if err != nil {
		return c, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c, false
	}
	if json.Unmarshal(data, &c) != nil {
		return Default(), false
	}
	if c.CaptureShortcut == "" {
		c.CaptureShortcut = DefaultShortcut
	}
	if ValidateShortcut(c.CaptureShortcut) != nil {
		c.CaptureShortcut = DefaultShortcut
		return c, true
	}
	return c, false
}

// Save writes the configuration, replacing the file in one step so a crash
// cannot leave half of it.
func (c Config) Save() error {
	if err := ValidateShortcut(c.CaptureShortcut); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return errors.New("无法确定配置文件路径")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.New("创建配置目录失败: " + err.Error())
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return errors.New("保存临时配置失败: " + err.Error())
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return errors.New("保存临时配置失败: " + err.Error())
	}
	if err := f.Close(); err != nil {
		return errors.New("保存临时配置失败: " + err.Error())
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return errors.New("替换配置文件失败: " + err.Error())
	}
	return nil
}

// ValidateShortcut checks that s is modifiers and one key, in accelerator
// syntax ("Super+Shift+S"). Whether the system lets the app have the keys
// is for the registration to tell.
func ValidateShortcut(s string) error {
	_, err := shortcutkey.Parse(s, runtime.GOOS)
	return err
}

// DisplayShortcut is the shortcut as the user reads it: Win and Ctrl, or the
// Mac's own names.
func DisplayShortcut(s string) string {
	parts := strings.Split(s, "+")
	for i, p := range parts {
		switch strings.ToLower(p) {
		case "commandorcontrol", "cmdorctrl":
			parts[i] = map[bool]string{true: "Cmd", false: "Ctrl"}[runtime.GOOS == "darwin"]
		case "super", "meta", "win", "cmd", "command":
			parts[i] = map[bool]string{true: "Cmd", false: "Win"}[runtime.GOOS == "darwin"]
		case "control":
			parts[i] = "Ctrl"
		}
	}
	return strings.Join(parts, "+")
}
