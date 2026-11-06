package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/doc"
)

func TestFileLaunchLoadsImagesAndExistingProjects(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 16, 8))); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "图片.png")
	if err := os.WriteFile(path, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := readDocument(path)
	if err != nil || l.title != "图片" || l.image.Width != 16 || l.pixels == nil || l.mosaic == nil {
		t.Fatalf("loaded=%+v, err=%v", l, err)
	}
	an := doc.New(doc.ToolText, doc.Point{}, doc.DefaultStyle(doc.ToolText))
	an.SetText("工程文字")
	created := doc.Timestamp(time.Unix(0, 0))
	project, err := doc.EncodeProject("已有工程", created, l.image, []doc.Annotation{an}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "已有工程.screenpin")
	if err := os.WriteFile(path, project, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err = readDocument(path)
	if err != nil || l.title != "已有工程" || l.createdAt != created || len(l.list) != 1 || l.list[0].TextValue() != "工程文字" {
		t.Fatalf("loaded=%+v, err=%v", l, err)
	}
	if err := os.WriteFile(path, []byte(`{"format":"screenpin-project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocument(path); err == nil {
		t.Fatal("file launch accepted a corrupt project")
	}
}

func TestExportSnapshotKeepsTheCapturedPenGeometry(t *testing.T) {
	a := New(config.Default())
	a.pixels = image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			a.pixels.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	pen := doc.New(doc.ToolPen, doc.Point{X: 10, Y: 20}, doc.DefaultStyle(doc.ToolPen))
	pen.Points = append(pen.Points, doc.Point{X: 50, Y: 20})
	a.history.Reset([]doc.Annotation{pen})
	export := a.snapshot()
	// The editor continues a gesture after the export started.
	a.history.Present()[0].Points[0] = doc.Point{X: 80, Y: 80}
	a.history.Present()[0].Points[1] = doc.Point{X: 99, Y: 80}
	data, err := export()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	color := color.RGBAModel.Convert(img.At(30, 20)).(color.RGBA)
	if color.G > 150 || color.B > 150 {
		t.Fatalf("the export used subsequently mutated pen points: %v", color)
	}
}
