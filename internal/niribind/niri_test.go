package niribind

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	data := []byte("binds {\n Mod+Shift+S { spawn \"other-screenshot\"; }\n}\ninclude \"appearance.kdl\"\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "appearance.kdl"), []byte("layout { gaps 8; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestPreparationNeverWritesAndOverridesBothModifierSpellings(t *testing.T) {
	path, before := fixture(t)
	p, err := prepare(path, "/opt/Screen Pin/screenpin", "Super+Shift+S", func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatal("preparation changed the user's config")
	}
	if _, err := os.Stat(p.ShortcutPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preparation installed a binding before consent")
	}
	for _, want := range []string{`"Super+Shift+S"`, `"Mod+Shift+S"`, `spawn "/opt/Screen Pin/screenpin" "--capture"`} {
		if !strings.Contains(string(p.content), want) {
			t.Fatalf("missing %q in %s", want, p.content)
		}
	}
}

func TestInstallValidatesStagesAndRollsBackFailedPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "save failure"}[fail], func(t *testing.T) {
			path, before := fixture(t)
			originalInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			checked := false
			p, err := prepare(path, "/opt/screenpin", "Super+Shift+S", func(ctx context.Context, staged string) error {
				checked = true
				if staged == path {
					t.Fatal("validated the live file instead of a staged config")
				}
				got, _ := os.ReadFile(path)
				if !bytes.Equal(got, before) {
					t.Fatal("modified live config before validation")
				}
				if _, err := exec.LookPath("niri"); err == nil {
					return validate(ctx, staged)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			saveErr := errors.New("settings are read-only")
			err = p.Apply(context.Background(), func() error {
				got, _ := os.ReadFile(p.ShortcutPath)
				if !bytes.Equal(got, p.content) {
					t.Fatal("persistence cannot inspect the installed candidate")
				}
				if fail {
					return saveErr
				}
				return nil
			})
			if !checked || (fail && !errors.Is(err, saveErr)) || (!fail && err != nil) {
				t.Fatalf("checked=%v err=%v", checked, err)
			}
			got, _ := os.ReadFile(path)
			if fail {
				if !bytes.Equal(got, before) {
					t.Fatal("failed save removed the previous binding")
				}
				if _, err := os.Stat(p.ShortcutPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed save left the candidate binding")
				}
			} else {
				if !bytes.HasPrefix(got, before) || !bytes.Contains(got, []byte(marker)) {
					t.Fatal("installation rewrote unrelated config")
				}
				info, _ := os.Stat(path)
				if info.Mode().Perm() != originalInfo.Mode().Perm() {
					t.Fatal("config permissions changed")
				}
				backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "screenpin-backup-*", "config.kdl"))
				if len(backups) != 1 {
					t.Fatal("missing config backup")
				}
				backup, _ := os.ReadFile(backups[0])
				if !bytes.Equal(backup, before) {
					t.Fatal("backup differs from original")
				}
			}
		})
	}
}

func TestValidationCancellationAndConcurrentChangesLeaveConfigUntouched(t *testing.T) {
	for _, kind := range []string{"invalid", "cancelled", "edited"} {
		t.Run(kind, func(t *testing.T) {
			path, before := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, err := prepare(path, "/opt/screenpin", "Super+Shift+S", func(context.Context, string) error {
				switch kind {
				case "invalid":
					return errors.New("invalid config")
				case "cancelled":
					cancel()
				case "edited":
					return os.WriteFile(path, []byte("// user's newer config\n"), 0o640)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.Apply(ctx, func() error { t.Fatal("invalid or stale request persisted"); return nil }); err == nil {
				t.Fatal("request succeeded")
			}
			got, _ := os.ReadFile(path)
			if kind == "edited" {
				before = []byte("// user's newer config\n")
			}
			if !bytes.Equal(got, before) {
				t.Fatal("overwrote config after failure")
			}
			if _, err := os.Stat(p.ShortcutPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failure left a candidate")
			}
		})
	}
}

func TestChangingTheKeyRestoresEarlierActionAndKeepsOneInclude(t *testing.T) {
	path, before := fixture(t)
	for _, key := range []string{"Super+Shift+S", "Ctrl+Alt+A"} {
		p, err := prepare(path, "/opt/screenpin", key, func(context.Context, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Apply(context.Background(), func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	if !bytes.HasPrefix(got, before) || bytes.Count(got, []byte(marker)) != 1 {
		t.Fatal("repeated changes duplicated the include or changed original actions")
	}
	leaf, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "screenpin-shortcut.kdl"))
	if bytes.Contains(leaf, []byte("Shift+S")) || !bytes.Contains(leaf, []byte("Ctrl+Alt+A")) {
		t.Fatal("changing keys kept the old override")
	}
}

func TestCompositorModRespectsIncludesAndComments(t *testing.T) {
	path, _ := fixture(t)
	data := []byte("input { mod-key \"Alt\"; }\ninclude \"appearance.kdl\"\n// mod-key \"Super\"\n")
	mod, err := compositorMod(path, data, map[string]bool{})
	if err != nil || mod != "Alt" {
		t.Fatalf("mod=%s err=%v", mod, err)
	}
}

func TestModParsingIgnoresStringsAndHonorsStatementOrder(t *testing.T) {
	path, _ := fixture(t)
	cases := []struct{ data, want string }{
		{`spawn-at-startup "echo mod-key \"Alt\""`, ""},
		{"/* outer /* nested */ mod-key \"Alt\" */\ninput { mod-key \"Super\"; }", "Super"},
		{"input { mod-key \"Alt\"; }\ninclude \"appearance.kdl\"", "Alt"},
		{"include \"appearance.kdl\"; input { mod-key \"Alt\"; }", "Alt"},
		{"/-include \"missing.kdl\"\n/-input { mod-key \"Alt\"; }\ninput { /-mod-key \"Alt\"; }", ""},
		{"input { mod-key /-\"Alt\" \"Super\"; }", "Super"},
		{`layer-rule { match namespace=r#"^app-[^ "]+//mod-key "Alt"$"#; }; input { mod-key "Super"; }`, "Super"},
		{`input { mod-key r#"Alt"#; }`, "Alt"},
	}
	for _, tc := range cases {
		got, err := compositorMod(path, []byte(tc.data), map[string]bool{})
		if err != nil || got != tc.want {
			t.Fatalf("%s: mod=%q err=%v", tc.data, got, err)
		}
	}
}

func TestUpdatedManagedBindingRollsBackAndDetectsChangedIncludes(t *testing.T) {
	path, _ := fixture(t)
	noop := func(context.Context, string) error { return nil }
	p, err := prepare(path, "/opt/screenpin", "Super+Shift+S", noop)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Apply(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	root, _ := os.ReadFile(path)
	leaf, _ := os.ReadFile(p.ShortcutPath)
	next, err := prepare(path, "/opt/screenpin", "Ctrl+Alt+A", noop)
	if err != nil {
		t.Fatal(err)
	}
	saveErr := errors.New("save failed")
	if err = next.Apply(context.Background(), func() error { return saveErr }); !errors.Is(err, saveErr) {
		t.Fatal(err)
	}
	gotRoot, _ := os.ReadFile(path)
	gotLeaf, _ := os.ReadFile(p.ShortcutPath)
	if !bytes.Equal(root, gotRoot) || !bytes.Equal(leaf, gotLeaf) {
		t.Fatal("failed update did not restore both files")
	}
	changed, err := prepare(path, "/opt/screenpin", "Super+Shift+S", noop)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(path), "appearance.kdl"), []byte("input { mod-key \"Alt\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = changed.Apply(context.Background(), func() error { t.Fatal("applied with a different Mod key"); return nil }); err == nil {
		t.Fatal("included Mod key changed after confirmation")
	}
}

func TestManagedRecognitionDoesNotClaimAnotherKeyOrBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("niri desktop recognition")
	}
	if !Current() {
		t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	}
	path, _ := fixture(t)
	t.Setenv("NIRI_CONFIG", path)
	t.Setenv("NIRI_SOCKET", "")
	p, err := Prepare("Super+Shift+S")
	if err != nil {
		t.Fatal(err)
	}
	if Matches("Super+Shift+S") || Installed() {
		t.Fatal("uninstalled binding was reported as active")
	}
	p.validate = func(context.Context, string) error { return nil }
	if err = p.Apply(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !Matches("Super+Shift+S") || !Installed() || Matches("Ctrl+Shift+S") {
		t.Fatal("managed recognition did not verify the key")
	}
	leaf, _ := os.ReadFile(p.ShortcutPath)
	leaf = bytes.ReplaceAll(leaf, []byte(p.Command), []byte("/opt/other-screenpin"))
	if err = os.WriteFile(p.ShortcutPath, leaf, 0o600); err != nil {
		t.Fatal(err)
	}
	if Matches("Super+Shift+S") {
		t.Fatal("a binding for a different binary was reported as active")
	}
}

func TestCaptureAndScrollBindingsShareManagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.kdl")
	if err := os.WriteFile(path, []byte("binds {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := prepareShortcuts(path, "/usr/bin/screenpin", "Super+Shift+S", "Super+Shift+L", func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"Super+Shift+S" repeat=false { spawn "/usr/bin/screenpin" "--capture"; }`, `"Super+Shift+L" repeat=false { spawn "/usr/bin/screenpin" "--scroll"; }`, `"Mod+Shift+L"`} {
		if !strings.Contains(string(p.content), text) {
			t.Fatalf("missing %q in %s", text, p.content)
		}
	}
}
func TestRecordShortcutSharesManagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.kdl")
	if err := os.WriteFile(path, []byte("binds {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := prepareShortcuts(path, "/usr/bin/screenpin", "Super+Shift+S", "Super+Shift+L", func(context.Context, string) error { return nil }, "Super+Shift+R")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"--capture", "--scroll", "--record"} {
		if !strings.Contains(string(p.content), action) {
			t.Fatalf("missing %s in %s", action, p.content)
		}
	}
	if p.RecordKey != "Super+Shift+R" {
		t.Fatal(p.RecordKey)
	}
}
