// Package render draws a document, the image and its annotations, with a
// ui.Painter. The editor's canvas and the exported image draw through the
// same function, so what is exported is what was on screen.
package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/doc"
)

// Scene is what Draw draws, in image pixels.
type Scene struct {
	Image *ui.Bitmap
	// Mosaic is the image pixelated, nil while it is not ready: mosaics
	// then draw as opaque blocks, so nothing under them ever shows.
	Mosaic      *ui.Bitmap
	Width       int
	Height      int
	Annotations []doc.Annotation
	// Hidden is the ID of an annotation not to draw, the text being edited.
	Hidden      string
	CornerRatio float64
	// Dimmer reuses the highlight geometry across editor frames. Exports can
	// leave it nil; a cache belongs to one renderer and is not shared with workers.
	Dimmer *DimmerCache
}

// TextFont is the family of text annotations; "" is the system's.
const TextFont = ""

// Draw paints s with its top-left corner at the origin of p, one unit per
// image pixel. Transform p to place and zoom it.
func Draw(p *ui.Painter, s *Scene) {
	if radius := doc.RelativeCornerRadius(float64(s.Width), float64(s.Height), s.CornerRatio); radius > 0 {
		p.Clip(ui.Rect{W: float32(s.Width), H: float32(s.Height)}, float32(radius), func() { drawScene(p, s) })
		return
	}
	drawScene(p, s)
}

func drawScene(p *ui.Painter, s *Scene) {
	full := ui.Rect{W: float32(s.Width), H: float32(s.Height)}
	if s.Image != nil {
		p.Image(s.Image, full, ui.FillBox)
	}
	var areas []doc.Rect
	dim := 0.0
	for i := range s.Annotations {
		a := &s.Annotations[i]
		if a.Type == doc.ToolAreaHighlight {
			n := a.Normalized()
			areas = append(areas, doc.Rect{X: n.X, Y: n.Y, W: n.Width, H: n.Height})
			dim = math.Max(dim, a.Opacity)
		}
		if a.ID == s.Hidden {
			continue
		}
		drawAnnotation(p, s, a)
	}
	if len(areas) > 0 && dim > 0 {
		p.FillPath(s.Dimmer.pathFor(areas, s.Width, s.Height), ui.RGBA(0, 0, 0, float32(dim)))
	}
}

// Color parses #rrggbb and applies opacity.
func Color(hex string, opacity float64) ui.Color {
	if len(hex) != 7 || hex[0] != '#' {
		return ui.Color{}
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return ui.Color{}
	}
	return ui.RGBA(uint8(v>>16), uint8(v>>8), uint8(v), float32(opacity))
}

func f(v float64) float32 { return float32(v) }

func drawAnnotation(p *ui.Painter, s *Scene, a *doc.Annotation) {
	stroke := Color(a.Color, a.Opacity)
	width := f(a.StrokeWidth)
	switch a.Type {
	case doc.ToolRectangle:
		n := a.Normalized()
		path := RoundedRect(n.X, n.Y, n.Width, n.Height, a.CornerRadius())
		if a.Fill != "transparent" {
			p.FillPath(path, Color(a.Fill, a.Opacity))
		}
		if width > 0 {
			p.StrokePath(path, width, stroke)
		}
	case doc.ToolEllipse:
		n := a.Normalized()
		path := Ellipse(n.X+n.Width/2, n.Y+n.Height/2, n.Width/2, n.Height/2)
		if a.Fill != "transparent" {
			p.FillPath(path, Color(a.Fill, a.Opacity))
		}
		if width > 0 {
			p.StrokePath(path, width, stroke)
		}
	case doc.ToolArrow:
		// One path, so where the shaft meets the head is not painted twice
		// and a translucent arrow stays even.
		tip := a.End()
		angle := math.Atan2(tip.Y-a.Y, tip.X-a.X)
		size := math.Max(12, a.StrokeWidth*4)
		var path ui.Path
		path.MoveTo(f(a.X), f(a.Y)).LineTo(f(tip.X), f(tip.Y))
		path.MoveTo(f(tip.X-size*math.Cos(angle-math.Pi/6)), f(tip.Y-size*math.Sin(angle-math.Pi/6))).
			LineTo(f(tip.X), f(tip.Y)).
			LineTo(f(tip.X-size*math.Cos(angle+math.Pi/6)), f(tip.Y-size*math.Sin(angle+math.Pi/6)))
		p.StrokePath(&path, width, stroke)
	case doc.ToolPen, doc.ToolHighlighter:
		if len(a.Points) == 0 {
			return
		}
		var path ui.Path
		path.MoveTo(f(a.Points[0].X), f(a.Points[0].Y))
		if len(a.Points) == 1 {
			path.LineTo(f(a.Points[0].X)+0.01, f(a.Points[0].Y))
		}
		for _, pt := range a.Points[1:] {
			path.LineTo(f(pt.X), f(pt.Y))
		}
		p.StrokePath(&path, width, stroke)
	case doc.ToolText:
		DrawText(p, a, a.TextValue())
	case doc.ToolMosaic:
		n := a.Normalized()
		if n.Width <= 0 || n.Height <= 0 {
			return
		}
		box := ui.Rect{X: f(n.X), Y: f(n.Y), W: f(n.Width), H: f(n.Height)}
		if s.Mosaic == nil {
			p.Fill(box, ui.RGBA(0x20, 0x23, 0x26, 1), 0)
			return
		}
		p.Clip(box, 3, func() {
			p.Image(s.Mosaic, ui.Rect{W: float32(s.Width), H: float32(s.Height)}, ui.FillBox)
		})
	}
}

// TextSpan is how a text annotation's text is drawn.
func TextSpan(a *doc.Annotation, text string) ui.Span {
	return ui.Span{Text: text, Font: TextFont, Size: f(a.FontSize), Weight: 600, Color: Color(a.Color, a.Opacity)}
}

// DrawText draws text in the style of the text annotation a, one line per
// line of text, LineHeight font sizes apart.
func DrawText(p *ui.Painter, a *doc.Annotation, text string) {
	for i, line := range doc.TextLines(text) {
		if line == "" {
			continue
		}
		p.RichText(f(a.X), f(a.Y+float64(i)*a.FontSize*doc.LineHeight), 0, TextSpan(a, line))
	}
}

// kappa places the control points of a cubic Bézier quarter circle.
const kappa = 0.5522847498

// Ellipse returns the outline of an ellipse.
func Ellipse(cx, cy, rx, ry float64) *ui.Path {
	kx, ky := rx*kappa, ry*kappa
	var p ui.Path
	p.MoveTo(f(cx+rx), f(cy))
	p.CubeTo(f(cx+rx), f(cy+ky), f(cx+kx), f(cy+ry), f(cx), f(cy+ry))
	p.CubeTo(f(cx-kx), f(cy+ry), f(cx-rx), f(cy+ky), f(cx-rx), f(cy))
	p.CubeTo(f(cx-rx), f(cy-ky), f(cx-kx), f(cy-ry), f(cx), f(cy-ry))
	p.CubeTo(f(cx+kx), f(cy-ry), f(cx+rx), f(cy-ky), f(cx+rx), f(cy))
	p.Close()
	return &p
}

// RoundedRect returns the outline of a rectangle with corners of radius r,
// which a stroke centers on as SVG's does.
func RoundedRect(x, y, w, h, r float64) *ui.Path {
	r = math.Max(0, math.Min(r, math.Min(w, h)/2))
	k := r * (1 - kappa)
	var p ui.Path
	p.MoveTo(f(x+r), f(y))
	p.LineTo(f(x+w-r), f(y))
	p.CubeTo(f(x+w-k), f(y), f(x+w), f(y+k), f(x+w), f(y+r))
	p.LineTo(f(x+w), f(y+h-r))
	p.CubeTo(f(x+w), f(y+h-k), f(x+w-k), f(y+h), f(x+w-r), f(y+h))
	p.LineTo(f(x+r), f(y+h))
	p.CubeTo(f(x+k), f(y+h), f(x), f(y+h-k), f(x), f(y+h-r))
	p.LineTo(f(x), f(y+r))
	p.CubeTo(f(x), f(y+k), f(x+k), f(y), f(x+r), f(y))
	p.Close()
	return &p
}

// DimmerCache retains only the current highlight geometry, never image pixels
// or annotation history. Compare copied rectangles so an in-place drag also
// invalidates the path while hover, zoom and opacity changes can reuse it.
type DimmerCache struct {
	areas         []doc.Rect
	width, height int
	path          *ui.Path
}

func (c *DimmerCache) pathFor(areas []doc.Rect, width, height int) *ui.Path {
	if c == nil {
		return dimmerPath(areas, width, height)
	}
	if c.path == nil || c.width != width || c.height != height || !slices.Equal(c.areas, areas) {
		c.path = dimmerPath(areas, width, height)
		c.areas = append(c.areas[:0], areas...)
		c.width, c.height = width, height
	}
	return c.path
}

// Sweep horizontal bands and merge their covered X intervals. This avoids
// testing every rectangle against every cell of a Cartesian edge grid.
// All uncovered spans form one path, so overlaps never dim twice and adjacent
// spans meet without an antialiasing seam.
func dimmerPath(areas []doc.Rect, width, height int) *ui.Path {
	ys := []float64{0, float64(height)}
	clipped := make([]doc.Rect, 0, len(areas))
	for _, r := range areas {
		x0, x1 := clamp(math.Min(r.X, r.X+r.W), 0, float64(width)), clamp(math.Max(r.X, r.X+r.W), 0, float64(width))
		y0, y1 := clamp(math.Min(r.Y, r.Y+r.H), 0, float64(height)), clamp(math.Max(r.Y, r.Y+r.H), 0, float64(height))
		if x0 < x1 && y0 < y1 {
			clipped = append(clipped, doc.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0})
			ys = append(ys, y0, y1)
		}
	}
	slices.Sort(ys)
	ys = slices.Compact(ys)
	type interval struct{ lo, hi float64 }
	spans := make([]interval, 0, len(clipped))
	var p ui.Path
	add := func(x0, y0, x1, y1 float64) {
		p.MoveTo(f(x0), f(y0)).LineTo(f(x1), f(y0)).
			LineTo(f(x1), f(y1)).LineTo(f(x0), f(y1)).Close()
	}
	for j := 0; j+1 < len(ys); j++ {
		spans = spans[:0]
		for _, r := range clipped {
			if r.Y < ys[j+1] && r.Y+r.H > ys[j] {
				spans = append(spans, interval{r.X, r.X + r.W})
			}
		}
		slices.SortFunc(spans, func(a, b interval) int {
			if a.lo < b.lo {
				return -1
			}
			if a.lo > b.lo {
				return 1
			}
			return 0
		})
		x := 0.0
		for _, span := range spans {
			if span.lo > x {
				add(x, ys[j], span.lo, ys[j+1])
			}
			x = math.Max(x, span.hi)
		}
		if x < float64(width) {
			add(x, ys[j], float64(width), ys[j+1])
		}
	}
	return &p
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// MosaicBlock is the size of a mosaic's squares, in image pixels.
func MosaicBlock(width, height int) int {
	return max(8, int(math.Round(float64(min(width, height))/90)))
}

// Pixelate shrinks img to one pixel per mosaic square, each the average of
// the square's pixels. Drawn back at the image's size, pixelated, it is the
// mosaic.
func Pixelate(img *image.RGBA) *image.RGBA {
	b := img.Bounds()
	block := MosaicBlock(b.Dx(), b.Dy())
	w, h := (b.Dx()+block-1)/block, (b.Dy()+block-1)/block
	sums := make([][5]uint64, w*h)
	for y := 0; y < b.Dy(); y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+b.Dx()*4]
		cells := sums[(y/block)*w:]
		for x := 0; x < b.Dx(); x++ {
			s := &cells[x/block]
			px := row[x*4 : x*4+4]
			s[0] += uint64(px[0])
			s[1] += uint64(px[1])
			s[2] += uint64(px[2])
			s[3] += uint64(px[3])
			s[4]++
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, s := range sums {
		if n := s[4]; n > 0 {
			out.SetRGBA(i%w, i/w, color.RGBA{uint8(s[0] / n), uint8(s[1] / n), uint8(s[2] / n), uint8(s[3] / n)})
		}
	}
	return out
}

// ToRGBA returns img as *image.RGBA, converting it only when it is not one.
func ToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Rect.Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}
