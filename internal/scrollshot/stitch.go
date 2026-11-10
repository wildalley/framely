// Package scrollshot stitches settled viewports without platform capture code.
package scrollshot

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	xdraw "golang.org/x/image/draw"
)

var (
	ErrOverlap   = errors.New("未找到可靠的重叠区域，请向上滚回一些并放慢滚动")
	ErrAmbiguous = errors.New("画面内容重复，无法确定拼接位置；已保留此前结果")
	ErrReverse   = errors.New("检测到向上滚动，未追加重复内容")
	ErrSize      = errors.New("画面尺寸已变化，请完成当前长截图后重新开始")
	ErrLimit     = errors.New("长截图已达到长度或内存上限，请完成当前图片")
)

// A wide table may change only names/phone numbers when moving one full row;
// most dates, labels and whitespace remain identical.
const duplicateThreshold = 0.15

type Outcome struct {
	Added     int
	Duplicate bool
	Height    int
}
type undo struct {
	chunks              []*image.RGBA
	previous, footer    *image.RGBA
	top, bottom, height int
}
type Stitcher struct {
	chunks                     []*image.RGBA
	previous, footer           *image.RGBA
	top, bottom, height, width int
	last                       *undo
	MaxPixels                  int64
	MaxHeight                  int
	scrollbar                  image.Rectangle
}

func New() *Stitcher              { return &Stitcher{MaxPixels: 24_000_000, MaxHeight: 30000} }
func (s *Stitcher) Height() int   { return s.height }
func (s *Stitcher) CanUndo() bool { return s.last != nil }
func (s *Stitcher) Undo() bool {
	if s.last == nil {
		return false
	}
	u := s.last
	s.chunks, s.previous, s.footer, s.top, s.bottom, s.height = u.chunks, u.previous, u.footer, u.top, u.bottom, u.height
	s.last = nil
	return true
}
func (s *Stitcher) Release() {
	s.chunks, s.previous, s.footer, s.last = nil, nil, nil, nil
	s.height, s.width = 0, 0
	s.scrollbar = image.Rectangle{}
}

func (s *Stitcher) Add(frame *image.RGBA) (Outcome, error) {
	if frame == nil || frame.Bounds().Dx() < 32 || frame.Bounds().Dy() < 80 {
		return Outcome{}, ErrSize
	}
	w, h := frame.Bounds().Dx(), frame.Bounds().Dy()
	if s.previous == nil {
		if !s.fits(w, h) {
			return Outcome{}, ErrLimit
		}
		first := clone(frame, frame.Bounds())
		s.width, s.height = w, h
		s.previous = first
		s.chunks = []*image.RGBA{first}
		s.scrollbar = scrollbarStrip(first)
		return Outcome{Added: h, Height: h}, nil
	}
	if w != s.width || h != s.previous.Bounds().Dy() {
		return Outcome{Height: s.height}, ErrSize
	}
	if Difference(s.previous, frame) < duplicateThreshold {
		return Outcome{Duplicate: true, Height: s.height}, nil
	}
	a, b := sample(s.previous), sample(frame)
	columns := movingColumns(a, b)
	if len(columns) < 4 {
		return Outcome{Duplicate: true, Height: s.height}, nil
	}
	top, bottom := s.top, s.bottom
	if len(s.chunks) == 1 && s.last == nil {
		top, bottom = stationaryEdges(a, b)
	}
	forward := match(a, b, columns, top, bottom)
	reverse := match(b, a, columns, top, bottom)
	if reverse.valid && !reverse.ambiguous && (!forward.valid || reverse.score+1 < forward.score) {
		return Outcome{Height: s.height}, ErrReverse
	}
	if !forward.valid {
		return Outcome{Height: s.height}, ErrOverlap
	}
	if forward.ambiguous {
		return Outcome{Height: s.height}, ErrAmbiguous
	}
	delta := forward.shift
	if !s.fits(w, s.height+delta) {
		return Outcome{Height: s.height}, ErrLimit
	}
	s.last = &undo{chunks: append([]*image.RGBA(nil), s.chunks...), previous: s.previous, footer: s.footer, top: s.top, bottom: s.bottom, height: s.height}
	// Replace the overlapping tail at a seam inside the moving content.
	// Keeping only a newly revealed bottom strip duplicates sticky footers
	// and can cut through rows when blank row padding is mistaken for a bar.
	seam := (max(top, h/10) + h - max(bottom, h/12) - delta) / 2
	cut := s.height - h + seam + delta
	s.chunks = s.prefix(cut)
	s.top, s.bottom = top, bottom
	s.previous = clone(frame, frame.Bounds())
	s.chunks = append(s.chunks, s.previous.SubImage(image.Rect(0, seam, w, h)).(*image.RGBA))
	s.footer = nil
	s.height += delta
	return Outcome{Added: delta, Height: s.height}, nil
}

func (s *Stitcher) prefix(height int) []*image.RGBA {
	chunks := make([]*image.RGBA, 0, len(s.chunks)+1)
	for _, c := range s.chunks {
		if height <= 0 {
			break
		}
		if height >= c.Rect.Dy() {
			chunks = append(chunks, c)
			height -= c.Rect.Dy()
		} else {
			// Detach the small retained strip from the previous full viewport.
			chunks = append(chunks, clone(c, image.Rect(c.Rect.Min.X, c.Rect.Min.Y, c.Rect.Max.X, c.Rect.Min.Y+height)))
			break
		}
	}
	return chunks
}

func (s *Stitcher) fits(w, h int) bool {
	return w > 0 && h > 0 && h <= s.MaxHeight && int64(w)*int64(h) <= s.MaxPixels
}
func clone(src *image.RGBA, r image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), src, r.Min, draw.Src)
	return out
}

// Image is an immutable tile view, so previewing does not allocate a full long
// image on each accepted viewport. Only finishing allocates the final pixels.
func (s *Stitcher) Image() image.Image {
	chunks := append([]*image.RGBA(nil), s.chunks...)
	if s.footer != nil {
		chunks = append(chunks, s.footer)
	}
	ends := make([]int, len(chunks))
	y := 0
	for i, c := range chunks {
		y += c.Bounds().Dy()
		ends[i] = y
	}
	return tiled{chunks: chunks, ends: ends, bounds: image.Rect(0, 0, s.width, s.height), scrollbar: s.scrollbar}
}
func (s *Stitcher) Flatten() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	y := 0
	for _, c := range s.chunks {
		r := image.Rect(0, y, s.width, y+c.Rect.Dy())
		draw.Draw(out, r, c, c.Rect.Min, draw.Src)
		y += c.Rect.Dy()
	}
	if s.footer != nil {
		draw.Draw(out, image.Rect(0, y, s.width, s.height), s.footer, s.footer.Rect.Min, draw.Src)
	}
	if !s.scrollbar.Empty() {
		for y := range s.height {
			background := out.RGBAAt(max(0, s.scrollbar.Min.X-2), y)
			for x := s.scrollbar.Min.X; x < s.scrollbar.Max.X; x++ {
				out.SetRGBA(x, y, background)
			}
		}
	}
	return out
}
func (s *Stitcher) Preview(w, h int) *image.RGBA {
	ratio := math.Min(1, math.Min(float64(w)/float64(max(1, s.width)), float64(h)/float64(max(1, s.height))))
	out := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(s.width)*ratio)), max(1, int(float64(s.height)*ratio))))
	view := s.Image()
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), view, view.Bounds(), draw.Src, nil)
	return out
}

type tiled struct {
	chunks    []*image.RGBA
	ends      []int
	bounds    image.Rectangle
	scrollbar image.Rectangle
}

func (t tiled) ColorModel() color.Model { return color.RGBAModel }
func (t tiled) Bounds() image.Rectangle { return t.bounds }
func (t tiled) At(x, y int) color.Color {
	if !image.Pt(x, y).In(t.bounds) {
		return color.RGBA{}
	}
	if !t.scrollbar.Empty() && x >= t.scrollbar.Min.X && x < t.scrollbar.Max.X {
		x = max(0, t.scrollbar.Min.X-2)
	}
	i := sort.SearchInts(t.ends, y+1)
	if i >= len(t.chunks) {
		return color.RGBA{}
	}
	start := 0
	if i > 0 {
		start = t.ends[i-1]
	}
	c := t.chunks[i]
	return c.RGBAAt(c.Rect.Min.X+x, c.Rect.Min.Y+y-start)
}

// scrollbarStrip recognizes only a narrow neutral thumb at the right edge,
// with a long light track around it. Text, colored graphics and full-height
// borders do not qualify. Filling from the neighboring gutter keeps width
// unchanged and prevents multiple thumb fragments in the finished image.
func scrollbarStrip(frame *image.RGBA) image.Rectangle {
	w, h := frame.Rect.Dx(), frame.Rect.Dy()
	if w < 80 || h < 160 {
		return image.Rectangle{}
	}
	type column struct{ x, top, length int }
	var columns []column
	for x := max(2, w-56); x < w; x++ {
		start, run, bestTop, best, light := 0, 0, 0, 0, 0
		for y := range h {
			c := frame.RGBAAt(frame.Rect.Min.X+x, frame.Rect.Min.Y+y)
			neutral := abs(int(c.R)-int(c.G)) <= 5 && abs(int(c.R)-int(c.B)) <= 5
			if neutral && c.R >= 232 {
				light++
			}
			if neutral && c.R >= 70 && c.R <= 210 {
				if run == 0 {
					start = y
				}
				run++
				if run > best {
					bestTop, best = start, run
				}
			} else {
				run = 0
			}
		}
		if light >= h/4 && best >= max(32, h/20) && best < h*3/4 {
			columns = append(columns, column{x, bestTop, best})
		}
	}
	for i := 0; i < len(columns); {
		j := i + 1
		for j < len(columns) && columns[j].x == columns[j-1].x+1 && abs(columns[j].top-columns[i].top) <= 3 && abs(columns[j].length-columns[i].length) <= 6 {
			j++
		}
		if j-i >= 3 && j-i <= 16 {
			// Include the rounded/antialiased edge of the recognized thumb.
			return image.Rect(max(2, columns[i].x-2), 0, min(w, columns[j-1].x+3), h)
		}
		i = j
	}
	return image.Rectangle{}
}

// Difference ignores isolated cursor/animation pixels by sampling the image.
func Difference(a, b *image.RGBA) float64 {
	if a == nil || b == nil || a.Bounds().Size() != b.Bounds().Size() {
		return math.Inf(1)
	}
	var total, count int64
	for y := 0; y < a.Rect.Dy(); y += max(1, a.Rect.Dy()/120) {
		for x := 0; x < a.Rect.Dx(); x += max(1, a.Rect.Dx()/160) {
			ca, cb := a.RGBAAt(a.Rect.Min.X+x, a.Rect.Min.Y+y), b.RGBAAt(b.Rect.Min.X+x, b.Rect.Min.Y+y)
			total += int64(abs(int(gray(ca)) - int(gray(cb))))
			count++
		}
	}
	return float64(total) / float64(max(int64(1), count))
}
func gray(c color.RGBA) uint8 { return uint8((77*uint32(c.R) + 150*uint32(c.G) + 29*uint32(c.B)) >> 8) }
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type features struct {
	rows []uint8
	ink  []bool
	n, h int
}

func sample(img *image.RGBA) features {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	left, right := max(1, w/100), w-max(10, w/40)
	n := min(192, right-left)
	f := features{rows: make([]uint8, n*h), ink: make([]bool, n*h), n: n, h: h}
	for y := range h {
		for i := range n {
			x := left + i*max(1, right-left-1)/max(1, n-1)
			f.rows[y*n+i] = gray(img.RGBAAt(img.Rect.Min.X+x, img.Rect.Min.Y+y))
		}
	}
	for y := range h {
		for x := range n {
			near := []int{int(f.rows[max(0, y-12)*n+x]), int(f.rows[max(0, y-6)*n+x]), int(f.rows[min(h-1, y+6)*n+x]), int(f.rows[min(h-1, y+12)*n+x])}
			sort.Ints(near)
			background := (near[1] + near[2]) / 2
			f.ink[y*n+x] = abs(int(f.rows[y*n+x])-background) >= 10
		}
	}
	return f
}
func movingColumns(a, b features) []int {
	var cols []int
	for x := range a.n {
		changed := 0
		for y := 0; y < a.h; y += 2 {
			if abs(int(a.rows[y*a.n+x])-int(b.rows[y*b.n+x])) > 8 {
				changed++
			}
		}
		if changed >= max(2, a.h/60) {
			cols = append(cols, x)
		}
	}
	return cols
}
func rowError(a, b features, ay, by int, cols []int) float64 {
	sum, count := 0, 0
	for _, x := range cols {
		if !a.ink[ay*a.n+x] && !b.ink[by*b.n+x] {
			continue
		}
		sum += abs(int(a.rows[ay*a.n+x]) - int(b.rows[by*b.n+x]))
		count++
	}
	return float64(sum) / float64(max(1, count))
}
func stationaryEdges(a, b features) (int, int) {
	cols := make([]int, a.n)
	for i := range cols {
		cols[i] = i
	}
	top, bottom := 0, 0
	for top < a.h/4 && rowError(a, b, top, top, cols) < 1.5 {
		top++
	}
	for bottom < a.h/4 && rowError(a, b, a.h-bottom-1, a.h-bottom-1, cols) < 1.5 {
		bottom++
	}
	return top, bottom
}

type alignment struct {
	shift            int
	score            float64
	valid, ambiguous bool
}

func match(a, b features, cols []int, top, bottom int) alignment {
	start, end := max(top, a.h/10), a.h-max(bottom, a.h/12)
	minOverlap := max(32, a.h/6)
	var rows []int
	for y := start; y < end; y += 2 {
		lo, hi := 255, 0
		for _, x := range cols {
			v := int(b.rows[y*b.n+x])
			lo, hi = min(lo, v), max(hi, v)
		}
		if hi-lo >= 18 {
			rows = append(rows, y)
		}
	}
	if len(rows) < 6 {
		return alignment{}
	}
	// Scores are evaluated serially and only the average escapes. Reuse the
	// small scratch buffer across shifts instead of allocating for each one.
	var scratch [28]float64
	score := func(d int) float64 {
		count := sort.SearchInts(rows, end-d)
		if count < 6 {
			return math.Inf(1)
		}
		n := min(28, count)
		values := scratch[:n]
		for i := range n {
			y := rows[i*(count-1)/max(1, n-1)]
			values[i] = rowError(a, b, y+d, y, cols)
		}
		sort.Float64s(values)
		keep := max(6, n-n/6)
		sum := 0.0
		for _, v := range values[:keep] {
			sum += v
		}
		return sum / float64(keep)
	}
	maxShift := end - start - minOverlap
	if maxShift < 1 {
		return alignment{}
	}
	best := alignment{score: math.Inf(1)}
	coarse := make([]alignment, 0, maxShift)
	for d := 1; d <= maxShift; d++ {
		v := score(d)
		coarse = append(coarse, alignment{shift: d, score: v})
		if v < best.score {
			best.shift, best.score = d, v
		}
	}
	sort.Slice(coarse, func(i, j int) bool { return coarse[i].score < coarse[j].score })
	for _, candidate := range coarse[:min(5, len(coarse))] {
		for d := max(1, candidate.shift-2); d <= min(maxShift, candidate.shift+2); d++ {
			if v := score(d); v < best.score {
				best.shift, best.score = d, v
			}
		}
	}
	if best.score > 8 {
		return alignment{}
	}
	second := math.Inf(1)
	for _, candidate := range coarse {
		if abs(candidate.shift-best.shift) > 3 {
			second = math.Min(second, candidate.score)
		}
	}
	best.valid = true
	best.ambiguous = second-best.score < math.Max(0.65, best.score*0.25)
	return best
}
