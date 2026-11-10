// Package config keeps ScreenPin's settings in config.json, in the place
// and format the previous version used, so they carry over.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wildalley/screenpin/internal/shortcutkey"
)

// DefaultShortcut is the global capture shortcut until the user records
// another: Win+Shift+S, as on Windows.
const DefaultShortcut = "Super+Shift+S"
const DefaultScrollShortcut = "Super+Shift+L"
const DefaultRecordShortcut = "Super+Shift+R"

// Config is the user's settings.
type Config struct {
	CaptureShortcut string `json:"captureShortcut"`
	ScrollShortcut  string `json:"scrollShortcut,omitempty"`
	RecordShortcut  string `json:"recordShortcut,omitempty"`
	// HideAfterCopy closes an unpinned image after copying. The JSON key stays
	// compatible with existing installations; closed images are not retained.
	HideAfterCopy   bool `json:"hideAfterCopy"`
	AutomaticScroll bool `json:"automaticScroll"`
}

// Default is the configuration of a first launch.
func Default() Config {
	return Config{CaptureShortcut: DefaultShortcut, AutomaticScroll: runtime.GOOS == "windows"}
}

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
		repaired = true
	}
	if c.Validate() != nil {
		c.ScrollShortcut = ""
		c.RecordShortcut = ""
		repaired = true
	}
	return c, repaired
}

func (c Config) Validate() error {
	capture, err := shortcutkey.Parse(c.CaptureShortcut, runtime.GOOS)
	if err != nil {
		return err
	}
	seen := map[string]bool{capture.Canonical(): true}
	for _, action := range []struct{ key, name string }{{c.ScrollShortcut, "长截图"}, {c.RecordShortcut, "录屏"}} {
		if action.key == "" {
			continue
		}
		key, err := shortcutkey.Parse(action.key, runtime.GOOS)
		if err != nil {
			return fmt.Errorf("%s：%w", action.name, err)
		}
		if seen[key.Canonical()] {
			return errors.New("截图、长截图与录屏不能使用同一个快捷键")
		}
		seen[key.Canonical()] = true
	}
	return nil
}

// Save writes the configuration, replacing the file in one step so a crash
// cannot leave half of it.
func (c Config) Save() error {
	if err := c.Validate(); err != nil {
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

// DisplayShortcut uses the platform's own modifier names in text and inputs.
func DisplayShortcut(s string) string {
	parts := strings.Split(s, "+")
	for i, p := range parts {
		switch strings.ToLower(p) {
		case "commandorcontrol", "cmdorctrl":
			parts[i] = map[bool]string{true: "Cmd", false: "Ctrl"}[runtime.GOOS == "darwin"]
		case "super", "meta", "win", "cmd", "command":
			switch runtime.GOOS {
			case "darwin":
				parts[i] = "Cmd"
			case "windows":
				parts[i] = "Win"
			default:
				parts[i] = "Super"
			}
		case "control":
			parts[i] = "Ctrl"
		}
	}
	return strings.Join(parts, "+")
}
