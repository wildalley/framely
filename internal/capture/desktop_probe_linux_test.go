//go:build linux

package capture

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/wildalley/framely/internal/portal"
)

// This opt-in desktop probe deliberately displays the real system chooser.
// It never grants permission or selects a source for the user.
func TestLiveDesktopStream(t *testing.T) {
	if os.Getenv("SCREENPIN_LIVE_CAPTURE") != "1" {
		t.Skip("set SCREENPIN_LIVE_CAPTURE=1 to test the real desktop chooser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source, err := OpenStream(ctx, StreamOptions{})
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, portal.ErrCancelled) {
		t.Skip("source selection was cancelled; frame capture was not tested")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	frame, err := source.Next(ctx)
	if err != nil {
		source.Close()
		t.Fatalf("%v\n%s", err, source.(*processStream).log.String())
	}
	if frame.Bounds().Empty() {
		t.Fatal("empty screen frame")
	}
	t.Logf("Received real PipeWire frame: %dx%d", frame.Rect.Dx(), frame.Rect.Dy())
}
