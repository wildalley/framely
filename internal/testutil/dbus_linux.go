//go:build linux

// Package testutil supplies isolated services for protocol integration tests.
package testutil

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// SessionBus creates a private bus. Tests never call the user's desktop
// portal, alter its permission store, or display an authorization dialog.
func SessionBus(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon is required for the portal integration tests")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "bus.conf")
	// No service directories: a missing fake interface cannot accidentally
	// activate the real portal or another desktop service in the test bus.
	settings := `<busconfig><type>session</type><listen>unix:tmpdir=` + dir + `</listen><auth>EXTERNAL</auth><policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>`
	if err := os.WriteFile(config, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dbus-daemon", "--config-file="+config, "--nofork", "--print-address=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			address <- scanner.Text()
		} else {
			address <- ""
		}
	}()
	select {
	case value := <-address:
		if value == "" {
			_ = cmd.Wait()
			t.Fatalf("starting isolated D-Bus: %s", stderr.String())
		}
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", value)
	case <-time.After(5 * time.Second):
		t.Fatal("the isolated D-Bus daemon did not start")
	}
}
