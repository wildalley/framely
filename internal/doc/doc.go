// Package doc is ScreenPin's document model: the captured image, the
// annotations drawn over it and the .screenpin project file that keeps both.
// It knows nothing about windows or drawing.
package doc

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"slices"
)

// Tool is what a press on the canvas does.
type Tool string

const (
	ToolSelect        Tool = "select"
	ToolRectangle     Tool = "rectangle"
	ToolEllipse       Tool = "ellipse"
	ToolArrow         Tool = "arrow"
	ToolPen           Tool = "pen"
	ToolHighlighter   Tool = "highlighter"
	ToolAreaHighlight Tool = "area-highlight"
	ToolText          Tool = "text"
	ToolMosaic        Tool = "mosaic"
)

// DrawTools are the tools that create annotations, in toolbar order.
var DrawTools = []Tool{ToolRectangle, ToolEllipse, ToolArrow, ToolPen, ToolHighlighter, ToolAreaHighlight, ToolText, ToolMosaic}

// Freehand reports whether the tool records a stroke of points.
func (t Tool) Freehand() bool { return t == ToolPen || t == ToolHighlighter }

// Boxed reports whether the annotation is a box that a drag may draw with a
// negative width or height, normalized once the drag ends.
func (t Tool) Boxed() bool {
	return t == ToolRectangle || t == ToolEllipse || t == ToolAreaHighlight || t == ToolMosaic
}

// Point is a position in image pixels.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Annotation is one editable object. Its JSON matches the project files the
// previous (web) editor wrote, so old projects keep opening.
type Annotation struct {
	ID          string   `json:"id"`
	Type        Tool     `json:"type"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	Width       float64  `json:"width"`
	Height      float64  `json:"height"`
	EndX        *float64 `json:"endX,omitempty"`
	EndY        *float64 `json:"endY,omitempty"`
	Points      []Point  `json:"points,omitempty"`
	Text        *string  `json:"text,omitempty"`
	Color       string   `json:"color"`
	Fill        string   `json:"fill"`
	StrokeWidth float64  `json:"strokeWidth"`
	Opacity     float64  `json:"opacity"`
	FontSize    float64  `json:"fontSize"`
	CornerRatio *float64 `json:"cornerRatio,omitempty"`
}

// Style is the editable look of an annotation, kept per tool so each tool
// remembers its own setup.
type Style struct {
	Color       string
	StrokeWidth float64
	Opacity     float64
	FontSize    float64
	CornerRatio float64
}

// Colors are the swatches of the style panel.
var Colors = []string{"#e5484d", "#ff8a3d", "#f2c94c", "#1f9d73", "#2383e2", "#7c5cff", "#171717", "#ffffff"}

// DefaultStyle is the look a tool starts with.
func DefaultStyle(t Tool) Style {
	s := Style{Color: "#e5484d", StrokeWidth: 4, Opacity: 1, FontSize: 28}
	if t == ToolRectangle {
		s.CornerRatio = 0.1
	}
	switch t {
	case ToolHighlighter:
		s.Color, s.StrokeWidth, s.Opacity = "#f2c94c", 18, 0.45
	case ToolAreaHighlight:
		s.Color, s.StrokeWidth, s.Opacity = "#f2c94c", 2, 0.55
	}
	return s
}

// Style returns the annotation's look.
func (a *Annotation) Style() Style {
	ratio := 0.0
	if half := math.Min(math.Abs(a.Width), math.Abs(a.Height)) / 2; a.Type == ToolRectangle && half > 0 {
		ratio = a.CornerRadius() / half
	}
	return Style{Color: a.Color, StrokeWidth: a.StrokeWidth, Opacity: a.Opacity, FontSize: a.FontSize, CornerRatio: ratio}
}

// SetStyle replaces the annotation's look. Mosaics stay opaque: their
// pixelation is what hides the image, and a project with a translucent
// mosaic is rejected on load.
func (a *Annotation) SetStyle(s Style) {
	a.Color, a.StrokeWidth, a.Opacity, a.FontSize = s.Color, s.StrokeWidth, s.Opacity, s.FontSize
	if a.Type == ToolRectangle {
		a.CornerRatio = ptr(ClampCornerRatio(s.CornerRatio))
	}
	if a.Type == ToolMosaic {
		a.Opacity = 1
	}
}

// ClampCornerRatio keeps the proportion finite: 0 is square, 1 is a pill.
func ClampCornerRatio(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}
func RelativeCornerRadius(w, h, ratio float64) float64 {
	return math.Min(math.Abs(w), math.Abs(h)) / 2 * ClampCornerRatio(ratio)
}

func (a Annotation) CornerRadius() float64 {
	if a.CornerRatio == nil {
		return math.Min(4, math.Min(math.Abs(a.Width), math.Abs(a.Height))/2)
	}
	return RelativeCornerRadius(a.Width, a.Height, *a.CornerRatio)
}

// DefaultText is what a new text annotation says until it is edited.
const DefaultText = "输入文字"

// New starts an annotation of tool t at p.
func New(t Tool, p Point, s Style) Annotation {
	a := Annotation{ID: NewID(), Type: t, X: p.X, Y: p.Y, Fill: "transparent"}
	a.SetStyle(s)
	switch {
	case t == ToolArrow:
		a.EndX, a.EndY = ptr(p.X), ptr(p.Y)
	case t.Freehand():
		a.Points = []Point{p}
	case t == ToolText:
		a.Text = ptr(DefaultText)
		a.Width, a.Height = 180, a.FontSize*1.5
	}
	return a
}

// NewID returns a random identifier in the format of crypto.randomUUID.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func ptr[T any](v T) *T { return &v }

// Clone copies the annotation so that changing the copy leaves the original
// as it was, its points too.
func (a Annotation) Clone() Annotation {
	if a.EndX != nil {
		a.EndX = ptr(*a.EndX)
	}
	if a.EndY != nil {
		a.EndY = ptr(*a.EndY)
	}
	if a.Text != nil {
		a.Text = ptr(*a.Text)
	}
	if a.CornerRatio != nil {
		a.CornerRatio = ptr(*a.CornerRatio)
	}
	a.Points = slices.Clone(a.Points)
	return a
}

// TextValue returns the text of a text annotation, "" for others.
func (a *Annotation) TextValue() string {
	if a.Text == nil {
		return ""
	}
	return *a.Text
}

// SetText sets the text of a text annotation.
func (a *Annotation) SetText(s string) { a.Text = ptr(s) }

// End returns the tip of an arrow.
func (a *Annotation) End() Point {
	p := Point{a.X, a.Y}
	if a.EndX != nil {
		p.X = *a.EndX
	}
	if a.EndY != nil {
		p.Y = *a.EndY
	}
	return p
}

// SetEnd moves the tip of an arrow.
func (a *Annotation) SetEnd(p Point) { a.EndX, a.EndY = ptr(p.X), ptr(p.Y) }

// Normalized returns the annotation with a non-negative width and height,
// its box unchanged.
func (a Annotation) Normalized() Annotation {
	if a.Width < 0 {
		a.X += a.Width
		a.Width = -a.Width
	}
	if a.Height < 0 {
		a.Y += a.Height
		a.Height = -a.Height
	}
	return a
}

// Rect is a box in image pixels.
type Rect struct{ X, Y, W, H float64 }

// Contains reports whether p is inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

// Inset grows r by d on every side.
func (r Rect) Inset(d float64) Rect { return Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d} }

// MinSize is the smallest drawn box that is kept, and the smallest box a
// selection shows.
const MinSize = 8

// LineHeight is the distance between the baselines of a text annotation's
// lines, as a multiple of its font size.
const LineHeight = 1.3

// TextLines splits a text annotation into its lines.
func TextLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			end := i
			if end > start && s[end-1] == '\r' {
				end--
			}
			lines = append(lines, s[start:end])
			start = i + 1
		}
	}
	return append(lines, s[start:])
}

// Bounds is the box the selection outlines and the corner handles resize.
func (a *Annotation) Bounds() Rect {
	switch {
	case a.Type.Freehand() && len(a.Points) > 0:
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, p := range a.Points {
			minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
			minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		}
		return Rect{minX, minY, math.Max(MinSize, maxX-minX), math.Max(MinSize, maxY-minY)}
	case a.Type == ToolText:
		lines := TextLines(a.TextValue())
		longest := 1
		for _, l := range lines {
			longest = max(longest, len([]rune(l)))
		}
		return Rect{a.X, a.Y,
			math.Max(a.Width, float64(longest)*a.FontSize),
			math.Max(a.Height, float64(len(lines))*a.FontSize*LineHeight)}
	}
	return Rect{a.X, a.Y, math.Max(MinSize, a.Width), math.Max(MinSize, a.Height)}
}

// Moved returns the annotation shifted by dx, dy.
func (a Annotation) Moved(dx, dy float64) Annotation {
	a = a.Clone()
	a.X += dx
	a.Y += dy
	for i := range a.Points {
		a.Points[i].X += dx
		a.Points[i].Y += dy
	}
	if a.EndX != nil {
		*a.EndX += dx
	}
	if a.EndY != nil {
		*a.EndY += dy
	}
	return a
}

// Handle is a part of the selection that a drag resizes.
type Handle string

const (
	HandleNW    Handle = "nw"
	HandleNE    Handle = "ne"
	HandleSW    Handle = "sw"
	HandleSE    Handle = "se"
	HandleN     Handle = "n"
	HandleS     Handle = "s"
	HandleW     Handle = "w"
	HandleE     Handle = "e"
	HandleStart Handle = "start"
	HandleEnd   Handle = "end"
)

// Resized returns the annotation with handle h dragged by dx, dy from where
// it was in a.
func (a Annotation) Resized(h Handle, dx, dy float64) Annotation {
	a = a.Clone()
	switch h {
	case HandleStart:
		a.X += dx
		a.Y += dy
		return a
	case HandleEnd:
		e := a.End()
		a.SetEnd(Point{e.X + dx, e.Y + dy})
		return a
	}
	orig := a.Bounds()
	b := orig
	if h == HandleNW || h == HandleNE || h == HandleN {
		b.Y, b.H = orig.Y+dy, orig.H-dy
	}
	if h == HandleSW || h == HandleSE || h == HandleS {
		b.H = orig.H + dy
	}
	if h == HandleNW || h == HandleSW || h == HandleW {
		b.X, b.W = orig.X+dx, orig.W-dx
	}
	if h == HandleNE || h == HandleSE || h == HandleE {
		b.W = orig.W + dx
	}
	n := a
	n.X, n.Y, n.Width, n.Height = b.X, b.Y, b.W, b.H
	n = n.Normalized()
	switch {
	case a.Type.Freehand() && len(a.Points) > 0:
		sx := n.Width / math.Max(MinSize, orig.W)
		sy := n.Height / math.Max(MinSize, orig.H)
		for i, p := range a.Points {
			n.Points[i] = Point{n.X + (p.X-orig.X)*sx, n.Y + (p.Y-orig.Y)*sy}
		}
	case a.Type == ToolText:
		n.FontSize = math.Max(12, a.FontSize*(n.Height/math.Max(MinSize, orig.H)))
	}
	return n
}

// TooSmall reports whether a just-drawn annotation is a stray click rather
// than a shape, and should be dropped.
func (a *Annotation) TooSmall() bool {
	return a.Type != ToolText && math.Abs(a.Width) < MinSize && math.Abs(a.Height) < MinSize && len(a.Points) < 2
}

// HitTest returns the index of the topmost annotation at p, or -1. tolerance
// widens thin strokes, in image pixels.
func HitTest(list []Annotation, p Point, tolerance float64) int {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Hit(p, tolerance) {
			return i
		}
	}
	return -1
}

// Hit reports whether p is on the annotation. Boxes take presses inside
// them, as their transparent fill does in the SVG editor; lines and strokes
// take presses near them.
func (a *Annotation) Hit(p Point, tolerance float64) bool {
	half := a.StrokeWidth/2 + tolerance
	switch a.Type {
	case ToolRectangle, ToolAreaHighlight, ToolMosaic:
		n := a.Normalized()
		return Rect{n.X, n.Y, n.Width, n.Height}.Inset(half).Contains(p)
	case ToolEllipse:
		n := a.Normalized()
		rx, ry := n.Width/2+half, n.Height/2+half
		if rx <= 0 || ry <= 0 {
			return false
		}
		dx, dy := (p.X-(n.X+n.Width/2))/rx, (p.Y-(n.Y+n.Height/2))/ry
		return dx*dx+dy*dy <= 1
	case ToolArrow:
		return segmentDistance(p, Point{a.X, a.Y}, a.End()) <= half
	case ToolPen, ToolHighlighter:
		if len(a.Points) == 1 {
			return math.Hypot(p.X-a.Points[0].X, p.Y-a.Points[0].Y) <= half
		}
		for i := 1; i < len(a.Points); i++ {
			if segmentDistance(p, a.Points[i-1], a.Points[i]) <= half {
				return true
			}
		}
		return false
	case ToolText:
		b := a.Bounds()
		return b.Contains(p)
	}
	return false
}

func segmentDistance(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := dx*dx + dy*dy
	if l == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// Equal reports whether two annotation lists are the same, field by field.
func Equal(a, b []Annotation) bool {
	return slices.EqualFunc(a, b, func(x, y Annotation) bool {
		return x.ID == y.ID && x.Type == y.Type && x.X == y.X && x.Y == y.Y &&
			x.Width == y.Width && x.Height == y.Height &&
			eqPtr(x.EndX, y.EndX) && eqPtr(x.EndY, y.EndY) && eqPtr(x.Text, y.Text) &&
			eqPtr(x.CornerRatio, y.CornerRatio) &&
			x.Color == y.Color && x.Fill == y.Fill && x.StrokeWidth == y.StrokeWidth &&
			x.Opacity == y.Opacity && x.FontSize == y.FontSize && slices.Equal(x.Points, y.Points)
	})
}

func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
