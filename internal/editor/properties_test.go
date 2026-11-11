package editor

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func rectangleApp() *App {
	a := New(config.Default())
	a.image = &doc.Image{Width: 500, Height: 300}
	r := doc.New(doc.ToolRectangle, doc.Point{X: 20, Y: 20}, doc.DefaultStyle(doc.ToolRectangle))
	r.Width, r.Height = 200, 100
	a.history.Reset([]doc.Annotation{r})
	a.selected = r.ID
	return a
}

func TestStyleFlyoutFitsBesideTheRail(t *testing.T) {
	for _, size := range [][2]int{{minWidth, minHeight}, {760, 400}, {1100, 700}} {
		width, height := size[0], size[1]
		for _, dark := range []bool{false, true} {
			for _, scale := range []float32{1, 1.5, 2} {
				a := rectangleApp()
				tt := ui.NewTester(a.view, width, height)
				tt.SetDark(dark)
				tt.SetScale(scale)
				panel, ok := tt.Find("快捷属性")
				if !ok || panel.X < railWidth || panel.Y < topBarHeight || panel.Y+panel.H > float32(height)+.5 {
					t.Fatalf("%v the style flyout is missing or outside the stage: %+v", size, panel)
				}
				for _, label := range []string{"颜色 红色", "颜色 白色", "粗细 细", "粗细 粗", "边角 直角", "边角 大圆角", "不透明度", "更多样式"} {
					r, ok := tt.Find(label)
					if !ok || r.W <= 0 || r.X < panel.X || r.X+r.W > panel.X+panel.W+.5 || r.Y < panel.Y || r.Y+r.H > panel.Y+panel.H+.5 {
						t.Fatalf("%v scale=%g dark=%v %s=%+v panel=%+v found=%v", size, scale, dark, label, r, panel, ok)
					}
				}
				a.release()
			}
		}
	}
}

func TestStylePresetsRestyleTheSelectionInOneUndoStepEach(t *testing.T) {
	a := rectangleApp()
	defer a.release()
	a.OnScroll = func() { t.Fatal("a style click launched long capture") }
	tt := ui.NewTester(a.view, 900, 500)
	if r, ok := tt.Find("粗细 中"); !ok || r.W <= 0 {
		t.Fatal("the default stroke is not shown as the medium preset")
	}
	for _, label := range []string{"粗细 粗", "边角 直角", "颜色 蓝色"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	got := a.history.Present()[0]
	if got.StrokeWidth != 8 || got.CornerRadius() != 0 || got.Color != "#2383e2" {
		t.Fatalf("presets did not restyle the rectangle: stroke=%g corner=%g color=%s", got.StrokeWidth, got.CornerRadius(), got.Color)
	}
	if a.styles[doc.ToolRectangle].StrokeWidth != 8 {
		t.Fatal("the next rectangle does not keep the chosen stroke")
	}
	for i := 0; i < 3; i++ {
		a.undo()
	}
	got = a.history.Present()[0]
	if got.StrokeWidth != 4 || math.Abs(got.CornerRadius()-5) > 1e-9 || a.history.CanUndo() {
		t.Fatalf("each preset was not one undo step: stroke=%g corner=%g", got.StrokeWidth, got.CornerRadius())
	}
}

func TestStyleFlyoutFollowsToolAndSelection(t *testing.T) {
	a := rectangleApp()
	defer a.release()
	tt := ui.NewTester(a.view, 760, 400)
	if _, ok := tt.Find("快捷属性"); !ok {
		t.Fatal("a selected rectangle shows no style flyout")
	}
	a.selected = ""
	tt.Frame()
	if r, ok := tt.Find("快捷属性"); ok && r.W > 0 {
		t.Fatal("the style flyout stays over the canvas with nothing to style")
	}
	if err := tt.Click("文字 (T)"); err != nil {
		t.Fatal(err)
	}
	if r, ok := tt.Find("字号 中"); !ok || r.W <= 0 {
		t.Fatal("the text tool shows no font size presets")
	}
	// Clicking the active tool again puts its styles away, and back.
	if err := tt.Click("文字 (T)"); err != nil {
		t.Fatal(err)
	}
	if r, ok := tt.Find("快捷属性"); (ok && r.W > 0) || a.tool != doc.ToolText {
		t.Fatal("clicking the active tool did not hide its styles")
	}
	if err := tt.Click("文字 (T)"); err != nil {
		t.Fatal(err)
	}
	if r, ok := tt.Find("快捷属性"); !ok || r.W <= 0 {
		t.Fatal("clicking the active tool again did not bring its styles back")
	}
	if err := tt.Click("更多样式"); err != nil {
		t.Fatal(err)
	}
	if a.panel != PanelStyle {
		t.Fatal("更多样式 did not open the full style popover")
	}
}
