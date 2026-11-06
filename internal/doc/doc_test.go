package doc

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestProjectRoundTrip(t *testing.T) {
	im, err := DecodeImage(pngBytes(t, 40, 20), "shot")
	if err != nil {
		t.Fatal(err)
	}
	pen := New(ToolPen, Point{1, 2}, DefaultStyle(ToolPen))
	pen.Points = append(pen.Points, Point{5, 6})
	list := []Annotation{
		New(ToolArrow, Point{3, 4}, DefaultStyle(ToolArrow)),
		New(ToolText, Point{0, 0}, DefaultStyle(ToolText)),
		pen,
		New(ToolMosaic, Point{0, 0}, Style{Color: "#e5484d", StrokeWidth: 4, Opacity: 0.3, FontSize: 28}),
	}
	data, err := EncodeProject("标题", Timestamp(time.Unix(0, 0)), im, list, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p, back, err := DecodeProject(data)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(p.Annotations, list) || back.Width != 40 || back.Height != 20 || p.Title != "标题" {
		t.Fatalf("round trip changed the project: %+v", p)
	}
	if list[3].Opacity != 1 {
		t.Fatal("a mosaic must stay opaque")
	}
}

// A project the web editor wrote opens.
func TestDecodesWebEditorProject(t *testing.T) {
	im, _ := DecodeImage(pngBytes(t, 2, 2), "x")
	src := `{"format":"screenpin-project","version":1,"title":"t","createdAt":"2026-10-01T08:00:00.000Z",
	"updatedAt":"2026-10-01T08:00:00.000Z","image":{"dataUrl":"` + im.DataURL() + `","width":2,"height":2,"name":"x"},
	"annotations":[{"id":"a","type":"rectangle","x":0,"y":0,"width":1,"height":1,"endX":0,"endY":0,
	"color":"#e5484d","fill":"transparent","strokeWidth":4,"opacity":1,"fontSize":28}]}`
	if _, _, err := DecodeProject([]byte(src)); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsInvalidProjects(t *testing.T) {
	im, _ := DecodeImage(pngBytes(t, 2, 2), "x")
	base := func(mod func(m map[string]any)) []byte {
		m := map[string]any{
			"format": "screenpin-project", "version": 1, "title": "t",
			"createdAt": "2026-10-01T08:00:00.000Z", "updatedAt": "2026-10-01T08:00:00.000Z",
			"image":       map[string]any{"dataUrl": im.DataURL(), "width": 2, "height": 2, "name": "x"},
			"annotations": []any{},
		}
		mod(m)
		b, _ := json.Marshal(m)
		return b
	}
	cases := map[string]func(m map[string]any){
		"format":    func(m map[string]any) { m["format"] = "other" },
		"size":      func(m map[string]any) { m["image"].(map[string]any)["width"] = 3 },
		"not image": func(m map[string]any) { m["image"].(map[string]any)["dataUrl"] = "data:image/png;base64,AAAA" },
		"bad date":  func(m map[string]any) { m["createdAt"] = "yesterday" },
		"text field": func(m map[string]any) {
			m["annotations"] = []any{map[string]any{"id": "a", "type": "rectangle", "x": 0, "y": 0, "width": 1, "height": 1, "text": "x", "color": "#000000", "fill": "transparent", "strokeWidth": 1, "opacity": 1, "fontSize": 10}}
		},
		"duplicate": func(m map[string]any) {
			a := map[string]any{"id": "a", "type": "rectangle", "x": 0, "y": 0, "width": 1, "height": 1, "color": "#000000", "fill": "transparent", "strokeWidth": 1, "opacity": 1, "fontSize": 10}
			m["annotations"] = []any{a, a}
		},
	}
	for name, mod := range cases {
		if _, _, err := DecodeProject(base(mod)); err == nil {
			t.Errorf("%s: accepted an invalid project", name)
		}
	}
	if _, _, err := DecodeProject(base(func(map[string]any) {})); err != nil {
		t.Fatalf("rejected a valid project: %v", err)
	}
}

func TestHistoryTransactions(t *testing.T) {
	var h History
	a := New(ToolRectangle, Point{0, 0}, DefaultStyle(ToolRectangle))
	h.Commit([]Annotation{a})
	h.Begin()
	for i := 1; i <= 5; i++ {
		h.SetTransient([]Annotation{a.Moved(float64(i), 0)})
	}
	h.End()
	if got := h.Present()[0].X; got != 5 {
		t.Fatalf("x = %v", got)
	}
	h.Undo()
	if h.Present()[0].X != 0 {
		t.Fatal("a transaction should undo in one step")
	}
	h.Undo()
	if len(h.Present()) != 0 || h.CanUndo() {
		t.Fatal("undo past the first commit")
	}
	h.Redo()
	h.Redo()
	if h.Present()[0].X != 5 || h.CanRedo() {
		t.Fatal("redo did not come back")
	}
	h.Begin()
	h.End()
	if !h.CanUndo() || h.CanRedo() {
		t.Fatal("an empty transaction changed the stacks")
	}
}

func TestResizeAndHit(t *testing.T) {
	r := New(ToolRectangle, Point{10, 10}, DefaultStyle(ToolRectangle))
	r.Width, r.Height = 100, 50
	got := r.Resized(HandleNW, 120, 0)
	if got.X != 110 || got.Width != 20 {
		t.Fatalf("dragging nw past ne flips the box: %+v", got)
	}
	if !r.Hit(Point{50, 30}, 0) || r.Hit(Point{200, 30}, 0) {
		t.Fatal("box hit test")
	}
	arrow := New(ToolArrow, Point{0, 0}, DefaultStyle(ToolArrow))
	arrow.SetEnd(Point{100, 0})
	if !arrow.Hit(Point{50, 3}, 1) || arrow.Hit(Point{50, 20}, 1) {
		t.Fatal("arrow hit test")
	}
	pen := New(ToolPen, Point{0, 0}, DefaultStyle(ToolPen))
	pen.Points = append(pen.Points, Point{10, 10}, Point{20, 0})
	scaled := pen.Resized(HandleSE, 20, 10)
	if b := scaled.Bounds(); b.W != 40 || b.H != 20 {
		t.Fatalf("pen resize bounds %+v", b)
	}
	if pen.Points[1] != (Point{10, 10}) {
		t.Fatal("resize changed the original's points")
	}
}

func TestTextLines(t *testing.T) {
	if got := strings.Join(TextLines("a\r\nb\n"), "|"); got != "a|b|" {
		t.Fatalf("%q", got)
	}
}
