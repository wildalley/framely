//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wildalley/screenpin/internal/testutil"
)

type fakeRequest struct{ closed *atomic.Int32 }

func (r *fakeRequest) Close() *dbus.Error { r.closed.Add(1); return nil }

type fakePortal struct {
	conn   *dbus.Conn
	code   uint32
	never  bool
	closed atomic.Int32
}

func (p *fakePortal) Screenshot(sender dbus.Sender, _ string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	token := options["handle_token"].Value().(string)
	owner := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	// An older portal's handle differs from the predicted token path.
	path := dbus.ObjectPath(string(ObjectPath) + "/request/" + owner + "/legacy_" + token)
	if !p.never {
		// Deliberately emit before the method reply. Subscribing afterwards
		// would lose this response and wait until the request timed out.
		if err := p.conn.Emit(path, "org.freedesktop.portal.Request.Response", p.code,
			map[string]dbus.Variant{"uri": dbus.MakeVariant("file:///tmp/" + token + ".png")}); err != nil {
			return "", dbus.MakeFailedError(err)
		}
	}
	return path, nil
}

func testClient(t *testing.T, code uint32, never bool) (*Client, *fakePortal) {
	t.Helper()
	testutil.SessionBus(t)
	server, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	p := &fakePortal{conn: server, code: code, never: never}
	if err := server.Export(p, ObjectPath, "org.freedesktop.portal.Screenshot"); err != nil {
		t.Fatal(err)
	}
	// Register once before serving concurrent requests; godbus's default
	// export handler cannot safely add paths while dispatching methods.
	if err := server.ExportSubtree(&fakeRequest{&p.closed}, dbus.ObjectPath(string(ObjectPath)+"/request"), "org.freedesktop.portal.Request"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.RequestName(Destination, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	// This fake deliberately lacks Registry, like older desktop portals.
	client, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, p
}

func TestConcurrentRequestsKeepEarlyResponsesAndReturnedHandles(t *testing.T) {
	client, _ := testClient(t, 0, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			options := map[string]dbus.Variant{"interactive": dbus.MakeVariant(false)}
			result, err := client.Request(ctx, "org.freedesktop.portal.Screenshot", "Screenshot", []any{""}, options)
			if err != nil {
				t.Errorf("request: %v", err)
				return
			}
			if _, changed := options["handle_token"]; changed {
				t.Error("request changed its caller's options")
			}
			results <- result["uri"].Value().(string)
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for uri := range results {
		if seen[uri] {
			t.Fatalf("two requests consumed the same response: %s", uri)
		}
		seen[uri] = true
	}
	if len(seen) != 8 {
		t.Fatalf("received %d of 8 distinct responses", len(seen))
	}
}

func TestCancellationClosesThePortalRequest(t *testing.T) {
	client, service := testClient(t, 0, true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Request(ctx, "org.freedesktop.portal.Screenshot", "Screenshot", []any{""}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || service.closed.Load() != 1 {
		t.Fatalf("err=%v, closed=%d", err, service.closed.Load())
	}
}

func TestPortalRejectionIsNotReportedAsSuccess(t *testing.T) {
	for _, code := range []uint32{1, 2} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client, _ := testClient(t, code, false)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := client.Request(ctx, "org.freedesktop.portal.Screenshot", "Screenshot", []any{""}, nil)
			want := ErrFailed
			if code == 1 {
				want = ErrCancelled
			}
			if !errors.Is(err, want) || !strings.Contains(err.Error(), "Screenshot") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
