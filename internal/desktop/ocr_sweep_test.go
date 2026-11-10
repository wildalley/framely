package desktop

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func wordCenter(t *testing.T, tt *ui.Tester, label string) ui.Point {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing chip %s", label)
	}
	return ui.Point{X: r.X + r.W/2, Y: r.Y + r.H/2}
}

func TestOCRSweepSelectsFastMotionAndDoesNotRetoggle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v := newOCRWindow()
		v.setText("one two three four", false)
		tt := ui.NewTester(v.view, 760, 620)
		tt.SetDark(dark)
		a, b := wordCenter(t, tt, "文字块 1：one"), wordCenter(t, tt, "文字块 4：four")
		tt.Press(a.X, a.Y)
		tt.Move(b.X, b.Y) // Only one event: intermediate words must be selected.
		if v.preview != "one two three four" {
			t.Fatalf("sweep skipped words: %q", v.preview)
		}
		tt.Move(a.X, a.Y)
		tt.Release(a.X, a.Y)
		if v.preview != "one two three four" {
			t.Fatalf("backtracking/release toggled selection: %q", v.preview)
		}
		tt.Press(b.X, b.Y)
		tt.Move(a.X, a.Y)
		tt.Release(a.X, a.Y)
		if v.selected != 0 || v.sweep.pressed {
			t.Fatal("sweep from selected text did not clear it")
		}
		if err := tt.Click("文字块 1：one"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeySpace)
		if v.selected != 0 {
			t.Fatal("pointer capture broke the focused chip's keyboard toggle")
		}
	}
}

func TestOCRSweepClipsListAndStopsWhenProgressReplacesText(t *testing.T) {
	v := newOCRWindow()
	v.setText(strings.Repeat("one two three four\n", 100), false)
	tt := ui.NewTester(v.view, 400, 400)
	a, b := wordCenter(t, tt, "文字块 1：one"), wordCenter(t, tt, "文字块 2：two")
	tt.Press(a.X, a.Y)
	tt.Move(b.X, b.Y)
	v.setText("new row\n"+v.text, true)
	tt.Frame()
	before := v.preview
	tt.Release(390, 390)
	if v.preview != before || v.sweep.pressed {
		t.Fatal("progress remapped an active gesture onto different text")
	}
	v.selection.All(false)
	v.refreshSelection()
	tt.Frame()
	a = wordCenter(t, tt, "文字块 1：new")
	tt.Press(a.X, a.Y)
	tt.Move(a.X, 390)
	tt.Release(a.X, 390)
	// The footer and overscan rows below the viewport cannot select hidden text.
	if v.selected >= 20 {
		t.Fatalf("sweep selected hidden rows: %d", v.selected)
	}
	if len(v.hits) > 300 {
		t.Fatalf("hit testing defeated list virtualization: %d", len(v.hits))
	}
	v.release()
	if v.hits != nil || v.sweep.pressed {
		t.Fatal("closed window retained hit-test state")
	}
}

func TestOCRSweepAfterScrollingUsesCommittedGeometry(t *testing.T) {
	v := newOCRWindow()
	var text strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&text, "line%d left right\n", i)
	}
	v.setText(text.String(), false)
	tt := ui.NewTester(v.view, 400, 500)
	first := wordCenter(t, tt, "文字块 1：line0")
	tt.Scroll(first.X, first.Y, 0, 240)
	var chosen string
	for i := 1; i < 100; i++ {
		label := fmt.Sprintf("文字块 %d：line%d", i*3+1, i)
		if r, ok := tt.Find(label); ok && r.Y > first.Y && r.Y < first.Y+150 {
			chosen = fmt.Sprintf("line%d", i)
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if chosen == "" || v.preview != chosen {
		t.Fatalf("scroll selected stale token: wanted %q got %q", chosen, v.preview)
	}
}
