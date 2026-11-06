package doc

import "slices"

// historyLimit is how many steps undo goes back.
const historyLimit = 80

// History is the undo stack of a document's annotations.
//
// The lists it holds are values: nothing changes an annotation that is in
// one of them, it puts a changed copy in a new list instead (Moved, Resized
// and Clone copy). The one exception is the annotation a drag is drawing,
// which only the list of the open transaction holds, so that a long pen
// stroke can grow in place.
type History struct {
	past, future [][]Annotation
	present      []Annotation
	checkpoint   []Annotation
	open         bool
}

// Present returns the current annotations. Do not change them in place.
func (h *History) Present() []Annotation { return h.present }

// CanUndo reports whether there is a step to undo.
func (h *History) CanUndo() bool { return len(h.past) > 0 }

// CanRedo reports whether there is a step to redo.
func (h *History) CanRedo() bool { return len(h.future) > 0 }

func (h *History) push(list []Annotation) {
	h.past = append(h.past, list)
	if len(h.past) > historyLimit {
		h.past = slices.Delete(h.past, 0, len(h.past)-historyLimit)
	}
	h.future = nil
}

// Commit makes next the present as one undoable step.
func (h *History) Commit(next []Annotation) {
	if h.open {
		h.present = next
		return
	}
	if Equal(h.present, next) {
		return
	}
	h.push(h.present)
	h.present = next
}

// Begin opens a transaction: the changes until End are one step.
func (h *History) Begin() {
	if !h.open {
		h.open = true
		h.checkpoint = h.present
	}
}

// InTransaction reports whether Begin opened a transaction End did not close.
func (h *History) InTransaction() bool { return h.open }

// SetTransient shows next without making it a step of its own.
func (h *History) SetTransient(next []Annotation) { h.present = next }

// End closes the transaction, recording one step if anything changed.
func (h *History) End() {
	if !h.open {
		return
	}
	h.open = false
	start := h.checkpoint
	h.checkpoint = nil
	if !Equal(start, h.present) {
		h.push(start)
	}
}

// Undo goes back one step.
func (h *History) Undo() {
	h.End()
	if len(h.past) == 0 {
		return
	}
	h.future = append([][]Annotation{h.present}, h.future...)
	if len(h.future) > historyLimit {
		h.future = h.future[:historyLimit]
	}
	h.present = h.past[len(h.past)-1]
	h.past = h.past[:len(h.past)-1]
}

// Redo goes forward one step.
func (h *History) Redo() {
	h.End()
	if len(h.future) == 0 {
		return
	}
	h.past = append(h.past, h.present)
	h.present = h.future[0]
	h.future = h.future[1:]
}

// Reset starts the history anew with list.
func (h *History) Reset(list []Annotation) {
	*h = History{present: list}
}

// Replace returns a copy of list with the annotation at i replaced by a.
func Replace(list []Annotation, i int, a Annotation) []Annotation {
	next := slices.Clone(list)
	next[i] = a
	return next
}

// IndexOf returns the index of the annotation with id, or -1.
func IndexOf(list []Annotation, id string) int {
	return slices.IndexFunc(list, func(a Annotation) bool { return a.ID == id })
}
