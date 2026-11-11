package apppaths

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationPreservesSettingsAndRetiresOldDirectory(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "darwin"} {
		for _, oldName := range []string{"screenpin", "ScreenPin"} {
			t.Run(platform+"/"+oldName, func(t *testing.T) {
				root := t.TempDir()
				old := filepath.Join(root, oldName)
				put(t, filepath.Join(old, "config.json"), `{"captureKey":"Ctrl+Alt+A"}`)
				put(t, filepath.Join(old, "scroll-source.json"), "portal token")
				put(t, filepath.Join(old, "unknown/user-file"), "preserve")
				dir, err := configDir(root, platform)
				if err != nil {
					t.Fatal(err)
				}
				want := "Framely"
				if platform == "linux" {
					want = "framely"
				}
				if filepath.Base(dir) != want {
					t.Fatalf("directory: %s", dir)
				}
				for _, file := range []string{"config.json", "scroll-source.json", "unknown/user-file"} {
					if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := os.Stat(old); !os.IsNotExist(err) {
					t.Fatalf("old directory remains: %v", err)
				}
				if _, err := configDir(root, platform); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestExistingSettingsWinAndDeletedTokensAreNotImportedAgain(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "framely")
	put(t, filepath.Join(dir, "config.json"), "current")
	put(t, filepath.Join(root, "screenpin/config.json"), "old")
	put(t, filepath.Join(root, "screenpin/scroll-source.json"), "old token")
	put(t, filepath.Join(root, "screenpin/unknown"), "keep")
	if _, err := configDir(root, "linux"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(data) != "current" {
		t.Fatalf("settings overwritten: %s, %v", data, err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "legacy-config-backup-*/config/unknown"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("missing backup: %v, %v", backups, err)
	}
	if err := os.Remove(filepath.Join(dir, "scroll-source.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := configDir(root, "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scroll-source.json")); !os.IsNotExist(err) {
		t.Fatal("consumed token was imported again")
	}
}

func TestFreshConfigurationDoesNotCreateLegacyPaths(t *testing.T) {
	root := t.TempDir()
	dir, err := configDir(root, "linux")
	if err != nil || dir != filepath.Join(root, "framely") {
		t.Fatalf("%s: %v", dir, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected files: %v, %v", entries, err)
	}
}
