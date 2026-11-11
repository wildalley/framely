//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	Destination = "org.freedesktop.portal.Desktop"
	ObjectPath  = dbus.ObjectPath("/org/freedesktop/portal/desktop")
)

// Client owns a private connection so registering our identity never changes
// the connection MyGo uses. Go channels receive the signals independently of
// the GLib thread which happened to issue a request.
type Client struct {
	conn   *dbus.Conn
	tokens atomic.Uint64
}

func New() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("无法连接桌面门户: %w", err)
	}
	c := &Client{conn: conn}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = conn.Object(Destination, ObjectPath).CallWithContext(ctx,
		"org.freedesktop.host.portal.Registry.Register", 0, AppID, map[string]dbus.Variant{}).Err
	if err != nil && !registryUnavailable(err) {
		conn.Close()
		return nil, fmt.Errorf("登记 %s 门户标识失败: %w", brand.Name, err)
	}
	return c, nil
}

func registryUnavailable(err error) bool {
	var busErr dbus.Error
	if !errors.As(err, &busErr) {
		return false
	}
	switch busErr.Name {
	case "org.freedesktop.DBus.Error.UnknownMethod", "org.freedesktop.DBus.Error.UnknownInterface",
		"org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.portal.Error.AlreadyExists":
		return true
	}
	return false
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Call(ctx context.Context, destination string, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return c.conn.Object(destination, path).CallWithContext(ctx, method, 0, args...)
}

// Subscription receives the requested signals. Close removes both the bus
// match and channel registration; callers do not close Signals themselves.
type Subscription struct {
	Signals chan *dbus.Signal
	client  *Client
	options []dbus.MatchOption
	once    sync.Once
}

func (c *Client) Subscribe(options ...dbus.MatchOption) (*Subscription, error) {
	s := &Subscription{Signals: make(chan *dbus.Signal, 64), client: c, options: options}
	c.conn.Signal(s.Signals)
	if err := c.conn.AddMatchSignal(options...); err != nil {
		c.conn.RemoveSignal(s.Signals)
		return nil, err
	}
	return s, nil
}

func (s *Subscription) Close() {
	s.once.Do(func() {
		_ = s.client.conn.RemoveMatchSignal(s.options...)
		s.client.conn.RemoveSignal(s.Signals)
	})
}

// Request subscribes before calling the method: Response may precede the
// method reply, and older portals choose a different handle. Each request
// filters its returned handle so concurrent calls cannot mix their answers.
func (c *Client) Request(ctx context.Context, iface, method string, args []any, options map[string]dbus.Variant) (map[string]dbus.Variant, error) {
	sender := strings.ReplaceAll(strings.TrimPrefix(c.conn.Names()[0], ":"), ".", "_")
	namespace := dbus.ObjectPath(string(ObjectPath) + "/request/" + sender)
	sub, err := c.Subscribe(dbus.WithMatchSender(Destination),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"),
		dbus.WithMatchPathNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("监听门户响应失败: %w", err)
	}
	defer sub.Close()
	copyOptions := make(map[string]dbus.Variant, len(options)+1)
	for key, value := range options {
		copyOptions[key] = value
	}
	token := fmt.Sprintf("screenpin_%d_%d", os.Getpid(), c.tokens.Add(1))
	copyOptions["handle_token"] = dbus.MakeVariant(token)
	params := append(append([]any(nil), args...), copyOptions)
	var handle dbus.ObjectPath
	if err := c.Call(ctx, Destination, ObjectPath, iface+"."+method, params...).Store(&handle); err != nil {
		if ctx.Err() != nil {
			// Modern portals honor handle_token, so cancellation can close
			// the request even before the method reply supplies its path.
			c.closeRequest(dbus.ObjectPath(string(namespace) + "/" + token))
		}
		return nil, fmt.Errorf("门户 %s 失败: %w", method, err)
	}
	if !handle.IsValid() || !strings.HasPrefix(string(handle), string(namespace)+"/") {
		return nil, errors.New("门户返回了无效的请求路径")
	}
	for {
		select {
		case signal, ok := <-sub.Signals:
			if !ok {
				return nil, errors.New("桌面门户连接已关闭")
			}
			if signal.Path != handle {
				continue
			}
			var code uint32
			var result map[string]dbus.Variant
			if err := dbus.Store(signal.Body, &code, &result); err != nil {
				return nil, fmt.Errorf("门户响应格式无效: %w", err)
			}
			switch code {
			case 0:
				return result, nil
			case 1:
				return nil, fmt.Errorf("门户 %s: %w", method, ErrCancelled)
			default:
				return nil, fmt.Errorf("门户 %s（响应 %d）: %w", method, code, ErrFailed)
			}
		case <-ctx.Done():
			c.closeRequest(handle)
			return nil, ctx.Err()
		}
	}
}

func (c *Client) closeRequest(handle dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Call(ctx, Destination, handle, "org.freedesktop.portal.Request.Close").Err
}
