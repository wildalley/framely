// Package ocr runs offline recognition in a cancellable worker process.
package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/wildalley/screenpin/internal/components"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Line struct {
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence"`
	Box        [4]Point `json:"box"`
}
type Result struct {
	Lines []Line `json:"lines"`
	Model string `json:"model"`
	Error string `json:"error,omitempty"`
}

func (r Result) Text() string {
	lines := make([]string, len(r.Lines))
	for i, line := range r.Lines {
		lines[i] = line.Text
	}
	return strings.Join(lines, "\n")
}

func Recognize(ctx context.Context, img *image.RGBA) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if img == nil || img.Bounds().Empty() || int64(img.Bounds().Dx())*int64(img.Bounds().Dy()) > 32_000_000 {
		return Result{}, errors.New("识别图片尺寸超出范围")
	}
	worker, err := components.Find("screenpin-ocr")
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, worker)
	configureProcess(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	var out limitedBuffer
	out.limit = 16 << 20
	var log limitedBuffer
	log.limit = 16 << 10
	cmd.Stdout, cmd.Stderr = &out, &log
	if err = cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("启动 OCR 失败: %w", err)
	}
	writeErr := png.Encode(in, img)
	closeErr := in.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if writeErr != nil {
		return Result{}, fmt.Errorf("发送 OCR 图片失败: %w", writeErr)
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	if err != nil {
		return Result{}, fmt.Errorf("OCR 组件失败: %w\n%s", err, log.String())
	}
	if out.overflow {
		return Result{}, errors.New("OCR 结果超过支持范围")
	}
	var result Result
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		return Result{}, fmt.Errorf("OCR 结果格式无效: %w", err)
	}
	if result.Error != "" {
		return Result{}, errors.New(result.Error)
	}
	if err = validate(&result, img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
		return Result{}, err
	}
	result.Lines = mergeRows(result.Lines)
	return result, nil
}

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
	for i := range r.Lines {
		l := &r.Lines[i]
		l.Text = strings.TrimSpace(l.Text)
		if len(l.Text) > 100000 || math.IsNaN(l.Confidence) || math.IsInf(l.Confidence, 0) || l.Confidence < 0 || l.Confidence > 1 {
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
			if abs((a.Min.Y+a.Max.Y)-(b.Min.Y+b.Max.Y)) <= max(4, min(a.Dy(), b.Dy())/2) && b.Min.X >= a.Min.X {
				text := line.Text
				if b.Min.X < a.Max.X {
					text = trimOverlap(last.Text, text)
				} else {
					last.Text += " "
				}
				last.Text += text
				last.Confidence = math.Min(last.Confidence, line.Confidence)
				u := a.Union(b)
				last.Box = [4]Point{{float64(u.Min.X), float64(u.Min.Y)}, {float64(u.Max.X), float64(u.Min.Y)}, {float64(u.Max.X), float64(u.Max.Y)}, {float64(u.Min.X), float64(u.Max.Y)}}
				continue
			}
		}
		rows = append(rows, line)
	}
	return rows
}

func trimOverlap(left, right string) string {
	a, b := []rune(left), []rune(right)
	for n := min(len(a), len(b)); n >= 2; n-- {
		if string(a[len(a)-n:]) == string(b[:n]) {
			return string(b[n:])
		}
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
