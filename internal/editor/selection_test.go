package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func TestDrawingShapesImmediatelyAllowsMoveAndResize(t *testing.T) {
	for _, zoom := range []float64{0.25, 1, 2} {
		a := New(config.Default())
		a.tool = doc.ToolRectangle
		gesture := func(from, to doc.Point) {
			a.canvasInput(ui.InputEvent{Kind: ui.InputPointerDown, X: float32(from.X * zoom), Y: float32(from.Y * zoom)}, zoom)
			a.canvasInput(ui.InputEvent{Kind: ui.InputPointerMove, X: float32(to.X * zoom), Y: float32(to.Y * zoom)}, zoom)
			a.canvasInput(ui.InputEvent{Kind: ui.InputPointerUp, X: float32(to.X * zoom), Y: float32(to.Y * zoom)}, zoom)
		}
		gesture(doc.Point{X: 20, Y: 30}, doc.Point{X: 220, Y: 230})
		if a.tool != doc.ToolSelect || a.selectedAnnotation() == nil {
			t.Fatal("new rectangle requires an explicit select-tool click")
		}
		gesture(doc.Point{X: 120, Y: 130}, doc.Point{X: 130, Y: 145})
		an := a.history.Present()[0]
		if len(a.history.Present()) != 1 || an.X != 30 || an.Y != 45 {
			t.Fatalf("zoom=%g move created another shape: %+v", zoom, an)
		}
		gesture(doc.Point{X: 230, Y: 245}, doc.Point{X: 250, Y: 265})
		an = a.history.Present()[0]
		if an.Width != 220 || an.Height != 220 {
			t.Fatalf("zoom=%g immediate resize: %+v", zoom, an)
		}
		a.undo()
		if a.history.Present()[0].Width != 200 {
			t.Fatal("resize did not undo separately")
		}
		a.undo()
		if a.history.Present()[0].X != 20 {
			t.Fatal("move did not undo separately")
		}
		a.undo()
		if len(a.history.Present()) != 0 {
			t.Fatal("draw did not undo")
		}
		a.release()
	}
}

func TestFreehandStillSupportsConsecutiveStrokes(t *testing.T) {
	for _, tool := range []doc.Tool{doc.ToolPen, doc.ToolHighlighter} {
		a := New(config.Default())
		a.tool = tool
		for _, y := range []float32{20, 50} {
			a.canvasInput(ui.InputEvent{Kind: ui.InputPointerDown, X: 20, Y: y}, 1)
			a.canvasInput(ui.InputEvent{Kind: ui.InputPointerUp, X: 100, Y: y}, 1)
		}
		if a.tool != tool || a.selected != "" || len(a.history.Present()) != 2 {
			t.Fatal("freehand stopped continuous drawing")
		}
		a.release()
	}
}

func TestImageAndRectangleCornerControlsHaveIndependentUndo(t *testing.T) {
	a := New(config.Default())
	defer a.release()
	rect := doc.New(doc.ToolRectangle, doc.Point{X: 20, Y: 20}, doc.DefaultStyle(doc.ToolRectangle))
	rect.Width, rect.Height = 200, 100
	a.history.Reset([]doc.Annotation{rect})
	a.selected = rect.ID
	tt := ui.NewTester(func(c *ui.Context) { a.appearancePanel(c, a.activeStyle()) }, 300, 340)
	if err := tt.Click("矩形圆角 50%"); err != nil {
		t.Fatal(err)
	}
	if got := a.history.Present()[0].CornerRadius(); got != 25 {
		t.Fatal(got)
	}
	if err := tt.Click("截图"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("截图圆角 100%"); err != nil {
		t.Fatal(err)
	}
	if a.history.ImageCornerRatio() != 1 || a.history.Present()[0].CornerRadius() != 25 {
		t.Fatal("image and annotation styles were mixed")
	}
	a.undo()
	if a.history.ImageCornerRatio() != 0 || a.history.Present()[0].CornerRadius() != 25 {
		t.Fatal("undo image corner style modified the annotation")
	}
	a.undo()
	if a.history.Present()[0].CornerRadius() != 5 {
		t.Fatal("rectangle corner change was not undoable")
	}
	a.redo()
	a.redo()
	if a.history.ImageCornerRatio() != 1 || a.history.Present()[0].CornerRadius() != 25 {
		t.Fatal("corner redo lost changes")
	}
}

func TestScreenshotCornerControlsAreAvailableWithoutAnAnnotation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := New(config.Default())
		a.image = &doc.Image{Width: 500, Height: 300}
		tt := ui.NewTester(a.view, 420, 500)
		tt.SetDark(dark)
		if err := tt.Click("样式"); err != nil {
			t.Fatal(err)
		}
		r, ok := tt.Find("截图圆角 50%")
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 420.5 || r.Y+r.H > 500.5 {
			t.Fatalf("dark=%v image corner control is hidden: %+v", dark, r)
		}
		if err := tt.Click("截图圆角 50%"); err != nil {
			t.Fatal(err)
		}
		if a.history.ImageCornerRatio() != .5 {
			t.Fatal("image style required a selected annotation")
		}
		a.release()
	}
}
