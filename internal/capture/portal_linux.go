//go:build linux

package capture

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wildalley/screenpin/internal/imagelimit"
	"github.com/wildalley/screenpin/internal/portal"
)

var (
	portalMu     sync.Mutex
	portalClient *portal.Client
)

func screenshotPortal() (*portal.Client, error) {
	portalMu.Lock()
	defer portalMu.Unlock()
	if portalClient == nil {
		client, err := portal.New()
		if err != nil {
			return nil, err
		}
		portalClient = client
	}
	return portalClient, nil
}

// Screenshot waits off the UI thread for the user or desktop to respond.
func Screenshot(interactive bool) ([]byte, error) {
	return ScreenshotContext(context.Background(), interactive)
}

func ScreenshotContext(parent context.Context, interactive bool) ([]byte, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	client, err := screenshotPortal()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	result, err := client.Request(ctx, "org.freedesktop.portal.Screenshot", "Screenshot", []any{""}, map[string]dbus.Variant{
		"interactive": dbus.MakeVariant(interactive),
		"modal":       dbus.MakeVariant(interactive),
	})
	if errors.Is(err, portal.ErrCancelled) {
		return nil, ErrCancelled
	}
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}
	raw, ok := result["uri"].Value().(string)
	if !ok {
		return nil, errors.New("截图门户没有返回图片路径")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.Path == "" {
		return nil, errors.New("截图门户返回的不是本地文件")
	}
	data, err := imagelimit.ReadFile(u.Path)
	// The portal creates a temporary file for this request.
	_ = os.Remove(u.Path)
	if err != nil {
		return nil, fmt.Errorf("无法读取截图文件: %w", err)
	}
	return data, nil
}

func ScreenshotAccess() Access {
	return ScreenshotAccessContext(context.Background())
}

func ScreenshotAccessContext(parent context.Context) Access {
	if parent.Err() != nil {
		return AccessUnsupported
	}
	client, err := screenshotPortal()
	if err != nil {
		return AccessUnsupported
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var permissions map[string][]string
	var data dbus.Variant
	err = client.Call(ctx, "org.freedesktop.impl.portal.PermissionStore",
		"/org/freedesktop/impl/portal/PermissionStore", "org.freedesktop.impl.portal.PermissionStore.Lookup",
		"screenshot", "screenshot").Store(&permissions, &data)
	if err != nil || len(permissions[AppID]) == 0 {
		return AccessUnknown
	}
	for _, value := range permissions[AppID] {
		if value == "yes" {
			return AccessGranted
		}
	}
	return AccessDenied
}

// Close releases the screenshot connection during application shutdown.
func Close() {
	portalMu.Lock()
	defer portalMu.Unlock()
	if portalClient != nil {
		_ = portalClient.Close()
		portalClient = nil
	}
}
