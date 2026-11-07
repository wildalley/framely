package capture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNiriScreenCastDiagnosisHonorsDesktopOverride(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux portal configuration")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "xdg-desktop-portal")
	_ = os.MkdirAll(dir, 0700)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("portals.conf", "[preferred]\ndefault=hyprland;gtk\n")
	if issue := StreamConfigurationIssue(); !strings.Contains(issue, "ScreenCast=gnome") {
		t.Fatal(issue)
	}
	write("niri-portals.conf", "[preferred]\ndefault=hyprland;gtk\norg.freedesktop.impl.portal.ScreenCast=gnome;\n")
	if issue := StreamConfigurationIssue(); issue != "" {
		t.Fatal(issue)
	}
	write("niri-portals.conf", "# default=gnome\n[preferred]\ndefault=gtk\norg.freedesktop.impl.portal.ScreenCast=hyprland;gnome\n")
	if issue := StreamConfigurationIssue(); !strings.Contains(issue, "niri-portals.conf") {
		t.Fatal(issue)
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	if issue := StreamConfigurationIssue(); issue != "" {
		t.Fatal("misdiagnosed Hyprland:", issue)
	}
}
