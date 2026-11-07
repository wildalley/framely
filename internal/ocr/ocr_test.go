package ocr

import (
	"context"
	"errors"
	"image"
	"math"
	"strings"
	"testing"
)

func TestOCRValidationAndFragmentMerge(t *testing.T) {
	line := func(text string, x, right, y float64) Line {
		return Line{Text: text, Confidence: 0.9, Box: [4]Point{{x, y}, {right, y}, {right, y + 20}, {x, y + 20}}}
	}
	r := Result{Lines: []Line{line("hello Windows", 100, 240, 10), line("文字识别 hello", 10, 150, 11), line("下一行", 10, 80, 50)}}
	if err := validate(&r, 300, 100); err != nil {
		t.Fatal(err)
	}
	r.Lines = mergeRows(r.Lines)
	if got := r.Text(); !strings.Contains(got, "文字识别 hello Windows\n下一行") {
		t.Fatal(got)
	}
	r.Lines[0].Box[0].X = math.NaN()
	if validate(&r, 300, 100) == nil {
		t.Fatal("accepted invalid geometry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Recognize(ctx, image.NewRGBA(image.Rect(0, 0, 10, 10))); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
