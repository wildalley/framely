package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// FrameProtocol is the stream protocol of screenpin-capture and
// screenpin-record that this build speaks.
const FrameProtocol = "2"

type protocolKey struct {
	path string
	size int64
	mod  time.Time
}

var protocolCache sync.Map // protocolKey -> string

// FindProtocol finds a worker and checks that it speaks want. A worker left
// from an older install otherwise fails mid-capture with an opaque exit code.
// The answer is cached per file version, so a replaced worker is asked again.
func FindProtocol(name, want string) (string, error) {
	path, err := Find(name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := protocolKey{path, info.Size(), info.ModTime()}
	got, ok := protocolCache.Load(key)
	if !ok {
		got = askProtocol(path)
		protocolCache.Store(key, got)
	}
	if got != want {
		return "", fmt.Errorf("组件 %s 版本不匹配（协议 %q，需要 %q），请重新安装完整的程序与配套组件：%s", name, got, want, path)
	}
	return path, nil
}

func askProtocol(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--protocol-version")
	hideWindow(cmd)
	out, err := cmd.Output()
	value := strings.TrimSpace(string(out))
	if err != nil || len(value) > 8 || strings.ContainsAny(value, " \n") {
		return "未知"
	}
	return value
}
