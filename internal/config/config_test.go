package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestShortcutPairValidation(t *testing.T) {
	for _, tc := range []struct {
		scroll string
		valid  bool
	}{
		{"", true}, {DefaultScrollShortcut, true}, {"shift+win+s", false}, {"Ctrl+", false},
	} {
		cfg := Default()
		cfg.ScrollShortcut = tc.scroll
		if err := cfg.Validate(); (err == nil) != tc.valid {
			t.Fatalf("scroll=%q error=%v", tc.scroll, err)
		}
	}
}

func TestOldConfigDoesNotEnableOrLoseShortcuts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG configuration fixture")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, []byte(`{"captureShortcut":"Ctrl+Alt+A","hideAfterCopy":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, repaired := Load()
	if repaired || cfg.CaptureShortcut != "Ctrl+Alt+A" || cfg.ScrollShortcut != "" || !cfg.HideAfterCopy {
		t.Fatalf("old config changed: %+v", cfg)
	}
	cfg.ScrollShortcut = DefaultScrollShortcut
	if err = cfg.Save(); err != nil {
		t.Fatal(err)
	}
	got, repaired := Load()
	if repaired || got != cfg {
		t.Fatalf("roundtrip=%+v", got)
	}
}

func TestRecordShortcutValidationAndOptInDefault(t *testing.T) {
	cfg := Default()
	if cfg.RecordShortcut != "" {
		t.Fatal("recording must not claim a key without configuration")
	}
	cfg.ScrollShortcut = DefaultScrollShortcut
	for _, value := range []string{DefaultShortcut, DefaultScrollShortcut, "Shift+Win+S", "Ctrl+"} {
		cfg.RecordShortcut = value
		if cfg.Validate() == nil {
			t.Fatalf("accepted conflict/invalid record key %q", value)
		}
	}
	cfg.RecordShortcut = DefaultRecordShortcut
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
