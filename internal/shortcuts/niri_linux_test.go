//go:build linux

package shortcuts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/niribind"
)

func TestManagedNiriShortcutWorksWithoutCallingThePortal(t *testing.T) {
	if _, err := exec.LookPath("niri"); err != nil {
		t.Skip("niri config validator is not installed")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("NIRI_SOCKET", "")
	path := filepath.Join(t.TempDir(), "config.kdl")
	t.Setenv("NIRI_CONFIG", path)
	if err := os.WriteFile(path, []byte("binds {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	plan, err := niribind.Prepare(config.DefaultShortcut)
	if err != nil {
		t.Fatal(err)
	}
	provider := newPlatformProvider().(*waylandProvider)
	defer provider.Close()
	m := &Manager{provider: provider, capture: func(string) { t.Fatal("niri should invoke --capture instead of a second listener") }, save: func(config.Config) error { return nil }}
	defer m.Close()
	if err = plan.Apply(ctx, func() error { return m.Apply(ctx, config.Default()) }); err != nil {
		t.Fatal(err)
	}
	if provider.client != nil || !strings.Contains(m.Description(), "niri") {
		t.Fatal("managed desktop binding attempted the unsupported portal")
	}
	// A restart restores the saved desktop binding without asking again.
	m.Close()
	restarted := &Manager{provider: newPlatformProvider(), save: func(config.Config) error { t.Fatal("startup rewrote settings"); return nil }}
	defer restarted.Close()
	if err = restarted.Start(ctx, config.Default()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(restarted.Description(), "niri") {
		t.Fatal("restart lost the desktop binding")
	}
}
