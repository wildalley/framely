package doc

import (
	"runtime"
	"testing"
	"weak"

	"github.com/wildalley/screenpin/internal/imagelimit"
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

func TestHistoryPrunesBytesAndPreservesCurrentDocument(t *testing.T) {
	var h History
	// Shared stroke buffers still consume a conservative per-snapshot budget.
	points := make([]Point, 1<<20)
	for _, id := range []string{"first", "second", "third", "current"} {
		h.Commit([]Annotation{{ID: id, Type: ToolPen, Points: points}})
	}
	if h.retainedBytes() > imagelimit.HistoryBytes || len(h.past) != 1 || h.present[0].ID != "current" {
		t.Fatalf("unbounded history or lost current: %d, %d", h.retainedBytes(), len(h.past))
	}
	h.Undo()
	if h.present[0].ID != "third" || !h.CanRedo() {
		t.Fatal("nearest undo lost")
	}
	h.Redo()
	if h.present[0].ID != "current" || h.retainedBytes() > imagelimit.HistoryBytes {
		t.Fatal("redo broke budget/current")
	}
	h.Begin()
	h.SetTransient([]Annotation{{ID: "transaction", Type: ToolPen, Points: points}})
	h.End()
	if h.retainedBytes() > imagelimit.HistoryBytes {
		t.Fatal("transaction bypassed byte budget")
	}
}

func TestOversizedUndoSnapshotDoesNotRetainAbandonedPixels(t *testing.T) {
	var h History
	h.Commit([]Annotation{{ID: "huge", Points: make([]Point, 3<<20)}})
	h.Commit([]Annotation{{ID: "small"}})
	if h.CanUndo() || h.retainedBytes() != 0 || h.Present()[0].ID != "small" {
		t.Fatal("oversized snapshot retained")
	}
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
