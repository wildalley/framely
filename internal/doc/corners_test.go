package doc

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRectangleCornerRatioScalesClonesAndPreservesLegacy(t *testing.T) {
	a := New(ToolRectangle, Point{}, DefaultStyle(ToolRectangle))
	a.Width, a.Height = 200, 100
	s := a.Style()
	s.CornerRatio = 1
	a.SetStyle(s)
	if a.CornerRadius() != 50 || a.Resized(HandleS, 0, 100).CornerRadius() != 100 {
		t.Fatal("corner radius does not scale with the short side")
	}
	copy := a.Clone()
	*copy.CornerRatio = 0
	if a.CornerRadius() != 50 || Equal([]Annotation{a}, []Annotation{copy}) {
		t.Fatal("corner history shares mutable state or ignores changes")
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Annotation
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !validAnnotation(&decoded) || decoded.CornerRadius() != 50 {
		t.Fatal("corner ratio lost in document encoding")
	}
	decoded.CornerRatio = nil
	if decoded.CornerRadius() != 4 {
		t.Fatal("legacy fixed corner radius changed")
	}
	for _, v := range []float64{-1, 1.1, math.NaN(), math.Inf(1)} {
		decoded.CornerRatio = &v
		if validAnnotation(&decoded) {
			t.Fatal("accepted invalid corner ratio", v)
		}
	}
}

func TestImageCornersShareUndoHistoryWithAnnotations(t *testing.T) {
	var h History
	h.Begin()
	h.SetImageCornerRatio(.1)
	h.SetImageCornerRatio(.5)
	h.SetImageCornerRatio(1)
	h.End()
	a := New(ToolRectangle, Point{}, DefaultStyle(ToolRectangle))
	h.Commit([]Annotation{a})
	h.Undo()
	if len(h.Present()) != 0 || h.ImageCornerRatio() != 1 {
		t.Fatal("undo drawing changed image corners")
	}
	h.Undo()
	if h.ImageCornerRatio() != 0 || h.CanUndo() {
		t.Fatal("slider gesture did not undo in one step")
	}
	h.Redo()
	h.Redo()
	if h.ImageCornerRatio() != 1 || len(h.Present()) != 1 {
		t.Fatal("redo failed")
	}
	h.SetImageCornerRatio(.25)
	if h.CanRedo() {
		t.Fatal("new appearance change retained stale redo")
	}
	h.Reset(nil)
	if h.ImageCornerRatio() != 0 || h.CanUndo() {
		t.Fatal("new image kept the previous corner style")
	}
}
