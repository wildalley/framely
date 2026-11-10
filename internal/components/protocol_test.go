package components

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindProtocolRejectsStaleWorkers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script workers")
	}
	dir := t.TempDir()
	t.Setenv("SCREENPIN_COMPONENTS_DIR", dir)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("current", `[ "$1" = --protocol-version ] && echo 2`)
	write("stale", `echo "Expected an authorized PipeWire fd and node"; exit 2`)
	if _, err := FindProtocol("current", FrameProtocol); err != nil {
		t.Fatal(err)
	}
	if _, err := FindProtocol("stale", FrameProtocol); err == nil || !strings.Contains(err.Error(), "版本不匹配") {
		t.Fatalf("stale worker accepted: %v", err)
	}
}
