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

func TestTranslationRequiresRemoteHTTPS(t *testing.T) {
	cfg := Default()
	cfg.TranslateProvider = "openai"
	cfg.TranslateBaseURL = "http://api.example.com/v1"
	if cfg.Validate() == nil {
		t.Fatal("accepted remote HTTP")
	}
	for _, raw := range []string{"https://api.example.com/v1", "http://localhost:8080/v1", "http://[::1]:8080/v1"} {
		cfg.TranslateBaseURL = raw
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	cfg.TranslateProvider = "offline"
	cfg.TranslateBaseURL = "http://api.example.com/v1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unused endpoint blocked unrelated settings: %v", err)
	}
}

func TestInvalidTranslationURLDoesNotResetUnrelatedShortcuts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG configuration fixture")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"captureShortcut":"Ctrl+Alt+A","scrollShortcut":"Super+Shift+L","recordShortcut":"Super+Shift+R","translateBaseURL":"http://api.example.com/v1"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, repaired := Load()
	if repaired || cfg.ScrollShortcut != DefaultScrollShortcut || cfg.RecordShortcut != DefaultRecordShortcut || cfg.TranslateBaseURL != "http://api.example.com/v1" {
		t.Fatalf("unrelated settings changed: %+v repaired=%v", cfg, repaired)
	}
}
