//go:build windows

package capture

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

var getClipboardSequenceNumber = syscall.NewLazyDLL("user32.dll").NewProc("GetClipboardSequenceNumber")

func clipboardSequence() uintptr {
	n, _, _ := getClipboardSequenceNumber.Call()
	return n
}

// System opens the Snipping Tool's region selector, which puts the region
// on the clipboard, and returns it as PNG once it is there. readImage reads
// the clipboard's image (PNG, or nil). Cancelling or waiting 30 seconds
// gives up; it never falls back to the whole screen.
func System(readImage func() []byte) ([]byte, error) {
	before := clipboardSequence()
	if err := exec.Command("explorer.exe", "ms-screenclip:").Start(); err != nil {
		return nil, errors.New("无法启动系统截图: " + err.Error())
	}
	for range 300 {
		time.Sleep(100 * time.Millisecond)
		// The sequence number changes even when the new pixels equal the
		// previous screenshot's, which comparing the images would miss.
		if clipboardSequence() == before {
			continue
		}
		if png := readImage(); len(png) > 0 {
			return png, nil
		}
	}
	return nil, errors.New("截图已取消或等待超时")
}
