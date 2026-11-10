//go:build !windows

package recording

import "os/exec"

func hideWindow(*exec.Cmd) {}
