package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func TestCanceledCanvasDragDoesNotCommitOrDiscardRedo(t *testing.T) {
	a := New(config.Default())
	defer a.release()
	a.tool = doc.ToolRectangle
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerDown, X: 10, Y: 20}, 1)
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerUp, X: 210, Y: 220}, 1)
	a.undo()
	a.tool = doc.ToolRectangle
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerDown, X: 30, Y: 40}, 1)
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerMove, X: 300, Y: 300}, 1)
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerUp, X: 300, Y: 300, Canceled: true}, 1)
	if len(a.history.Present()) != 0 || a.history.CanUndo() || !a.history.CanRedo() || a.history.InTransaction() || a.drag != nil {
		t.Fatal("cancelled drawing committed a shape or discarded redo")
	}
	a.redo()
	want := a.history.Present()[0].Clone()
	a.tool = doc.ToolSelect
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerDown, X: 110, Y: 120}, 1)
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerMove, X: 160, Y: 180}, 1)
	a.canvasInput(ui.InputEvent{Kind: ui.InputPointerUp, X: 160, Y: 180, Canceled: true}, 1)
	if !doc.Equal(a.history.Present(), []doc.Annotation{want}) {
		t.Fatal("cancelled move changed the existing annotation")
	}
	a.undo()
	if len(a.history.Present()) != 0 {
		t.Fatal("cancelled move added an undo step")
	}
}

func TestCanceledScreenRegionDoesNotConfirm(t *testing.T) {
	v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}, crop: image.Rect(0, 0, 800, 600)}
	s := &pick{result: make(chan pickResult, 1), active: v, dragging: true, origin: doc.Point{X: 10, Y: 10}, corner: doc.Point{X: 210, Y: 220}, minimum: image.Pt(80, 160)}
	s.input(v, ui.InputEvent{Kind: ui.InputPointerUp, X: 210, Y: 220, Canceled: true}, 800, 600)
	if s.done || s.dragging || s.active != nil || len(s.result) != 0 {
		t.Fatal("cancelled screen selection confirmed or retained the drag")
	}
}

func TestCanceledImageRegionRestoresSelectionWithoutStartingCapture(t *testing.T) {
	for _, quick := range []bool{false, true} {
		p := &imagePicker{bounds: image.Rect(0, 0, 1000, 500), bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1000, 500))), result: make(chan regionChoice, 1), quick: quick, minimum: image.Pt(80, 160)}
		tt := ui.NewTester(p.view, 600, 500)
		box, _ := tt.Find("图片选区")
		r := p.imageRect
		tt.Press(box.X+r.X+r.W/4, box.Y+r.Y+r.H/10)
		tt.Move(box.X+r.X+r.W*3/4, box.Y+r.Y+r.H*9/10)
		tt.CancelPress()
		if p.done || p.dragging || !p.selection().Empty() || len(p.result) != 0 {
			t.Fatalf("quick=%v: cancelled image region was retained or started capture", quick)
		}
	}
}
