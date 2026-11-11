package editor

import (
	"context"
	"errors"
	"image"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func TestClosingImageReleasesStateAndRejectsPendingWork(t *testing.T) {
	a, annotation := textApp()
	b, other := textApp()
	defer b.release()
	a.pixels = image.NewRGBA(image.Rect(0, 0, 500, 300))
	a.mosaicPx = image.NewRGBA(a.pixels.Bounds())
	a.bitmap = ui.NewBitmap(a.pixels)
	a.mosaic = ui.NewBitmap(a.mosaicPx)
	a.history.Commit([]doc.Annotation{annotation.Moved(5, 5)})
	a.history.Begin()
	a.drag = &drag{id: annotation.ID}
	a.editingID, a.editingText, a.slidingKey = annotation.ID, "temporary text", "opacity"
	a.toasts = []string{"pending message"}
	a.OnPaste = func() { t.Fatal("a closed image kept its actions") }
	calls := 0
	a.OnClosed = func() { calls++ }
	pending := a.snapshot()
	a.release()
	a.release()
	if !a.closed || !errors.Is(a.ctx.Err(), context.Canceled) || calls != 1 {
		t.Fatal("closing is not final and idempotent")
	}
	if a.image != nil || a.pixels != nil || a.mosaicPx != nil || a.bitmap != nil || a.mosaic != nil || a.drag != nil ||
		a.styles != nil || a.toasts != nil || a.OnPaste != nil || a.OnClosed != nil || a.editingText != "" ||
		!reflect.DeepEqual(a.history, doc.History{}) {
		t.Fatal("a closed image retained its data or history")
	}
	applied := false
	a.accept(func() { applied = true })
	if applied {
		t.Fatal("a pending result revived the closed image")
	}
	if _, err := pending(); !errors.Is(err, context.Canceled) {
		t.Fatalf("pending export was not cancelled: %v", err)
	}
	if b.image == nil || b.ctx.Err() != nil || b.history.Present()[0].ID != other.ID {
		t.Fatal("closing one image affected another")
	}
}

func TestCancelledPickerReleasesDesktopPixels(t *testing.T) {
	full := image.NewRGBA(image.Rect(0, 0, 100, 100))
	s := &pick{full: full, bitmap: ui.NewBitmap(full), active: &pickView{}, result: make(chan pickResult, 1), dragging: true, shift: true}
	s.finish(nil, nil)
	s.finish(nil, nil)
	if !s.done || s.full != nil || s.bitmap != nil || s.active != nil || s.views != nil || s.dragging || s.shift {
		t.Fatal("a cancelled selector kept its screenshot")
	}
	if result := <-s.result; !errors.Is(result.err, capture.ErrCancelled) {
		t.Fatal(result.err)
	}
	if len(s.result) != 0 {
		t.Fatal("the picker finished twice")
	}
}

func TestCloseShortcutWorksDuringAnExport(t *testing.T) {
	a := New(config.Default())
	a.busy = true
	tt := ui.NewTester(a.shortcuts, 320, 200)
	tt.Key(ui.Cmd, ui.KeyW)
	if !a.closed || a.busy {
		t.Fatal("an image could not close while an export was pending")
	}
}
