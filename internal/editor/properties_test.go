package editor

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func TestStyleStripControlsShareTheSameVerticalCenter(t *testing.T) {
	for _, width := range []int{480, 760, 1100} {
		for _, scale := range []float32{1, 1.5, 2} {
			for _, dark := range []bool{false, true} {
				a := rectangleApp()
				tt := ui.NewTester(a.view, width, 400)
				tt.SetScale(scale)
				tt.SetDark(dark)
				labels := []string{"矩形圆角", "矩形圆角增减", "粗细", "不透明度"}
				var center float32
				for i, label := range labels {
					r, ok := tt.Find(label)
					if !ok {
						t.Fatalf("missing %s", label)
					}
					if i == 0 {
						center = r.Y + r.H/2
					}
					if math.Abs(float64(r.Y+r.H/2-center)) > .5 {
						t.Fatalf("width=%d scale=%g dark=%v %s center=%g, expected=%g", width, scale, dark, label, r.Y+r.H/2, center)
					}
				}
				a.release()
			}
		}
	}
}

func rectangleApp() *App {
	a := New(config.Default())
	a.image = &doc.Image{Width: 500, Height: 300}
	r := doc.New(doc.ToolRectangle, doc.Point{X: 20, Y: 20}, doc.DefaultStyle(doc.ToolRectangle))
	r.Width, r.Height = 200, 100
	a.history.Reset([]doc.Annotation{r})
	a.selected = r.ID
	return a
}

func TestStyleStripFitsTheStage(t *testing.T) {
	for _, width := range []int{480, 600, 759, 760, 900, 1100} {
		for _, dark := range []bool{false, true} {
			for _, scale := range []float32{1, 1.5, 2} {
				a := rectangleApp()
				tt := ui.NewTester(a.view, width, 400)
				tt.SetDark(dark)
				tt.SetScale(scale)
				row, ok := tt.Find("快捷属性")
				if !ok || row.X < railWidth || row.Y < topBarHeight {
					t.Fatalf("width=%d the style strip is missing or outside the stage: %+v", width, row)
				}
				labels := []string{"颜色", "矩形圆角", "粗细", "不透明度"}
				if width >= 560+railWidth {
					labels[0] = "颜色 #e5484d"
				}
				for _, label := range labels {
					r, ok := tt.Find(label)
					if !ok || r.X < row.X || r.X+r.W > float32(width)+.5 || r.Y < row.Y || r.Y+r.H > row.Y+row.H+.5 {
						t.Fatalf("width=%d scale=%g dark=%v %s=%+v row=%+v found=%v", width, scale, dark, label, r, row, ok)
					}
				}
				for i := 1; i < len(labels); i++ {
					before, _ := tt.Find(labels[i-1])
					after, _ := tt.Find(labels[i])
					if before.X+before.W > after.X+.5 {
						t.Fatalf("width=%d controls overlap: %s=%+v %s=%+v", width, labels[i-1], before, labels[i], after)
					}
				}
				a.release()
			}
		}
	}
}

func TestInlineNumberEditingIsOneUndoAndDoesNotTriggerCanvasCommands(t *testing.T) {
	a := rectangleApp()
	defer a.release()
	a.OnScroll = func() { t.Fatal("typing launched long capture") }
	tt := ui.NewTester(a.view, 900, 500)
	if err := tt.Click("矩形圆角"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.TypeKey(0, ui.KeyR, "r")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	if a.tool != doc.ToolSelect || len(a.history.Present()) != 1 {
		t.Fatal("numeric field editing changed canvas state")
	}
	tt.Type("50")
	if got := a.history.Present()[0].CornerRadius(); got != 25 {
		t.Fatal("number did not update the selected rectangle", got)
	}
	if err := tt.Click("选择 (V)"); err != nil {
		t.Fatal(err)
	}
	if a.history.InTransaction() {
		t.Fatal("leaving numeric input kept its transaction open")
	}
	a.undo()
	if a.history.Present()[0].CornerRadius() != 5 || a.history.CanUndo() {
		t.Fatal("typed numeric value was not one undo step")
	}
}

func TestStyleStripFollowsToolAndSelection(t *testing.T) {
	a := rectangleApp()
	defer a.release()
	tt := ui.NewTester(a.view, 760, 400)
	if _, ok := tt.Find("快捷属性"); !ok {
		t.Fatal("a selected rectangle shows no style strip")
	}
	a.selected = ""
	tt.Frame()
	if r, ok := tt.Find("快捷属性"); ok && r.W > 0 {
		t.Fatal("the style strip stays over the canvas with nothing to style")
	}
	a.setTool(doc.ToolText)
	tt.Frame()
	if _, ok := tt.Find("字号"); !ok {
		t.Fatal("the text tool shows no font size")
	}
}
