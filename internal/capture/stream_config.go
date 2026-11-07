package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// StreamConfigurationIssue diagnoses a known incompatible desktop/backend
// pairing without opening a portal or altering the user's configuration.
func StreamConfigurationIssue() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	niri := false
	for _, desktop := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(desktop, "niri") {
			niri = true
		}
	}
	if !niri {
		return ""
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"niri-portals.conf", "portals.conf"} {
		path := filepath.Join(dir, "xdg-desktop-portal", name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "无法读取屏幕共享配置：" + err.Error()
		}
		if preferredScreenCast(string(data)) == "hyprland" {
			return fmt.Sprintf("niri 的屏幕共享被配置为 Hyprland，来源选择器可能崩溃。请在 %s 的 [preferred] 中设置 org.freedesktop.impl.portal.ScreenCast=gnome，然后重新登录或重启桌面门户。", path)
		}
		return ""
	}
	return ""
}

func preferredScreenCast(data string) string {
	section, fallback, screen := "", "", ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[preferred]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		switch strings.TrimSpace(key) {
		case "default":
			fallback = value
		case "org.freedesktop.impl.portal.ScreenCast":
			screen = value
		}
	}
	if screen != "" {
		return screen
	}
	return fallback
}
