//go:build !windows

package ocr

import "os/exec"

func configureProcess(*exec.Cmd) {}
