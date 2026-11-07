package doc

import (
	"runtime"
	"testing"
	"weak"
)

func abandonedStrokeHistory() (*History, weak.Pointer[Point]) {
	h := &History{}
	points := make([]Point, 1<<16)
	pointer := weak.Make(&points[0])
	h.Commit([]Annotation{{ID: "large", Type: ToolPen, Points: points}})
	h.Commit([]Annotation{{ID: "replacement", Type: ToolRectangle}})
	h.Undo()
	h.Undo()
	// A new branch discards redo; the large stroke must now be collectible.
	h.Commit([]Annotation{{ID: "new-branch", Type: ToolRectangle}})
	return h, pointer
}

func TestDiscardedUndoBranchDoesNotRetainStrokePoints(t *testing.T) {
	h, pointer := abandonedStrokeHistory()
	runtime.GC()
	if pointer.Value() != nil {
		t.Fatal("an abandoned undo branch still retains its stroke buffer")
	}
	runtime.KeepAlive(h)
}

func TestConsumedHistorySlotsAreCleared(t *testing.T) {
	var h History
	h.Commit([]Annotation{{ID: "first"}})
	h.Commit([]Annotation{{ID: "second"}})
	h.Undo()
	if tail := h.past[:cap(h.past)][len(h.past):]; len(tail) != 0 && tail[0].annotations != nil {
		t.Fatal("popped undo slot still holds annotations")
	}
	storage := h.future
	h.Redo()
	if storage[0].annotations != nil {
		t.Fatal("consumed redo slot still holds annotations")
	}
}
