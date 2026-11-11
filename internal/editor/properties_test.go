package editor

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
)

func TestPropertyControlsShareTheSameVerticalCenter(t *testing.T) {
	for _, width := range []int{320, 420, 760, 900} {
		for _, scale := range []float32{1, 1.5, 2} {
			for _, dark := range []bool{false, true} {
				a := rectangleApp()
				a.propertyImage = true
				tt := ui.NewTester(a.view, width, 400)
				tt.SetScale(scale)
				tt.SetDark(dark)
				labels := []string{"属性目标", "截图圆角", "截图圆角增减", "快捷圆角 0%", "快捷圆角 50%", "快捷圆角 100%"}
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

func TestInlinePropertiesFitTheSecondRowWithoutScrolling(t *testing.T) {
	for _, width := range []int{320, 420, 600, 759, 760, 900, 1100} {
		for _, dark := range []bool{false, true} {
			for _, scale := range []float32{1, 1.5, 2} {
				a := rectangleApp()
				tt := ui.NewTester(a.view, width, 400)
				tt.SetDark(dark)
				tt.SetScale(scale)
				row, ok := tt.Find("快捷属性")
				if !ok {
					t.Fatal("missing property row")
				}
				labels := []string{"属性目标", "颜色", "矩形圆角", "粗细", "不透明度"}
				if width >= 900 {
					labels[1] = "颜色 #e5484d"
				}
				if width >= 600 {
					labels = append(labels, "截图圆角")
				}
				for _, label := range labels {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.X+r.W > float32(width)+.5 || r.Y < row.Y || r.Y+r.H > row.Y+row.H+.5 {
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
	if err := tt.Click("截图圆角"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("25")
	_ = tt.Click("选择 (V)")
	if a.history.ImageCornerRatio() != .25 || a.history.Present()[0].CornerRadius() != 5 {
		t.Fatal("image corner control restyled the annotation")
	}
}

func TestCompactPropertyTargetSwitchesBetweenRectangleAndImage(t *testing.T) {
	a := rectangleApp()
	defer a.release()
	tt := ui.NewTester(a.view, 320, 400)
	if err := tt.Click("属性目标"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("截图"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("截图圆角"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("75")
	_ = tt.Click("选择 (V)")
	if a.history.ImageCornerRatio() != .75 || a.history.Present()[0].CornerRadius() != 5 {
		t.Fatal("compact target selection changed the wrong object")
	}
}
