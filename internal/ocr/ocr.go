// Package ocr runs offline recognition in a cancellable worker process.
package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wildalley/framely/internal/imagelimit"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Line struct {
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence"`
	Box        [4]Point `json:"box"`
	Tile       int      `json:"tile,omitempty"`
}
type Result struct {
	Lines []Line `json:"lines"`
	Model string `json:"model"`
	Error string `json:"error,omitempty"`
}

// Each worker loads its own models and inference runtime (about 200 MiB in
// the Linux audit). Queue requests across image windows instead of loading
// several copies at once. Closing a queued image still cancels immediately.
var workerSlots = make(chan struct{}, 1)

func (r Result) Text() string {
	lines := make([]string, len(r.Lines))
	for i, line := range r.Lines {
		lines[i] = line.Text
	}
	return strings.Join(lines, "\n")
}

func Recognize(ctx context.Context, img *image.RGBA) (Result, error) {
	return RecognizePrepared(ctx, func(context.Context) (*image.RGBA, error) { return img, nil }, nil)
}

// Progress contains only newly recognized lines, never image pixels. Callbacks
// run on the requesting goroutine; UI clients must dispatch to their UI thread.
type Progress struct {
	Stage     string `json:"stage,omitempty"`
	Completed int    `json:"completed,omitempty"`
	Total     int    `json:"total,omitempty"`
	Lines     []Line `json:"lines,omitempty"`
}

// RecognizePrepared acquires the global slot before allocating a composite.
// A cancelled queued request never calls prepare or starts another worker.
func RecognizePrepared(ctx context.Context, prepare func(context.Context) (*image.RGBA, error), progress func(Progress)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if progress != nil {
		progress(Progress{Stage: "queued"})
	}
	select {
	case workerSlots <- struct{}{}:
		defer func() { <-workerSlots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if progress != nil {
		progress(Progress{Stage: "preparing"})
	}
	img, err := prepare(ctx)
	prepare = nil
	if err != nil {
		return Result{}, err
	}
	if img == nil || imagelimit.Dimensions(img.Rect.Dx(), img.Rect.Dy()) != nil {
		return Result{}, errors.New("识别图片尺寸超出范围")
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := sharedWorker.recognize(ctx, img, progress)
	if err != nil {
		return Result{}, err
	}
	if err = validate(&result, img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
		return Result{}, err
	}
	result.Lines = MergeLines(result.Lines)
	return result, nil
}

// MergeLines preserves independent table cells. Only overlapping fragments
// from different horizontal tiles may be joined; legacy (tile=0) output uses
// the same overlap requirement. Input is copied because partial UI results
// can still be in use by the caller.
func MergeLines(lines []Line) []Line { return mergeRows(append([]Line(nil), lines...)) }

func bounds(l Line) image.Rectangle {
	minX, minY, maxX, maxY := l.Box[0].X, l.Box[0].Y, l.Box[0].X, l.Box[0].Y
	for _, p := range l.Box {
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

func validate(r *Result, w, h int) error {
	if len(r.Lines) > 20000 {
		return errors.New("OCR 返回的文本行过多")
	}
	textBytes := 0
	for i := range r.Lines {
		l := &r.Lines[i]
		l.Text = strings.TrimSpace(l.Text)
		textBytes += len(l.Text)
		if textBytes > 2<<20 || len(l.Text) > 100000 || math.IsNaN(l.Confidence) || math.IsInf(l.Confidence, 0) || l.Confidence < 0 || l.Confidence > 1 {
			return errors.New("OCR 返回了无效文本或置信度")
		}
		for _, p := range l.Box {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || p.X < 0 || p.Y < 0 || p.X > float64(w) || p.Y > float64(h) {
				return errors.New("OCR 返回的文字位置超出图片")
			}
		}
	}
	return nil
}

// mergeRows joins horizontally tiled line fragments in visual reading order.
func mergeRows(lines []Line) []Line {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := bounds(lines[i]), bounds(lines[j])
		return a.Min.Y+a.Max.Y < b.Min.Y+b.Max.Y
	})
	// A tile may detect the right-hand fragment a few pixels above the left.
	// Group by baseline before sorting horizontally, so these still join.
	for start := 0; start < len(lines); {
		a := bounds(lines[start])
		end := start + 1
		for end < len(lines) {
			b := bounds(lines[end])
			if abs((a.Min.Y+a.Max.Y)-(b.Min.Y+b.Max.Y)) > max(4, min(a.Dy(), b.Dy())/2) {
				break
			}
			end++
		}
		sort.SliceStable(lines[start:end], func(i, j int) bool { return bounds(lines[start+i]).Min.X < bounds(lines[start+j]).Min.X })
		start = end
	}
	var rows []Line
	for _, line := range lines {
		if line.Text == "" {
			continue
		}
		if len(rows) > 0 {
			last := &rows[len(rows)-1]
			a, b := bounds(*last), bounds(line)
			crossTile := last.Tile != line.Tile || last.Tile == 0
			if crossTile && abs((a.Min.Y+a.Max.Y)-(b.Min.Y+b.Max.Y)) <= max(4, min(a.Dy(), b.Dy())/2) && b.Min.X >= a.Min.X && b.Min.X < a.Max.X {
				text := line.Text
				text = trimOverlap(last.Text, text)
				if text == line.Text {
					rows = append(rows, line)
					continue
				}
				last.Text += text
				last.Confidence = math.Min(last.Confidence, line.Confidence)
				u := a.Union(b)
				last.Box = [4]Point{{float64(u.Min.X), float64(u.Min.Y)}, {float64(u.Max.X), float64(u.Min.Y)}, {float64(u.Max.X), float64(u.Max.Y)}, {float64(u.Min.X), float64(u.Max.Y)}}
				last.Tile = line.Tile
				continue
			}
		}
		rows = append(rows, line)
	}
	return rows
}

func trimOverlap(left, right string) string {
	a, b := []rune(left), []rune(right)
	if len(b) < 2 {
		return right
	}
	// Prefix matching is linear even for long repeated text. Comparing every
	// suffix separately lets a malformed component stall result processing.
	prefix := make([]int, len(b))
	for i, n := 1, 0; i < len(b); i++ {
		for n > 0 && b[i] != b[n] {
			n = prefix[n-1]
		}
		if b[i] == b[n] {
			n++
		}
		prefix[i] = n
	}
	n := 0
	for _, r := range a {
		for n > 0 && (n == len(b) || r != b[n]) {
			n = prefix[n-1]
		}
		if r == b[n] {
			n++
		}
	}
	if n >= 2 {
		return string(b[n:])
	}
	return right
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := b.limit - b.Len()
	if room < len(p) {
		b.overflow = true
		p = p[:max(0, room)]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
