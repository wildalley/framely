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
	"github.com/wildalley/screenpin/internal/doc"
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
	Hidden string
}

// TextFont is the family of text annotations; "" is the system's.
const TextFont = ""

// Draw paints s with its top-left corner at the origin of p, one unit per
// image pixel. Transform p to place and zoom it.
func Draw(p *ui.Painter, s *Scene) {
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
		p.FillPath(dimmerPath(areas, s.Width, s.Height), ui.RGBA(0, 0, 0, float32(dim)))
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
		path := RoundedRect(n.X, n.Y, n.Width, n.Height, 4)
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

// dimmerPath covers the image apart from the highlighted areas. The areas
// may overlap, which an even-odd or winding hole cannot cut out once each,
// so the complement is split along every area's edges into cells, and the
// cells no area covers are filled: all of them in one path, so that
// neighboring cells meet without a seam.
func dimmerPath(areas []doc.Rect, width, height int) *ui.Path {
	xs := []float64{0, float64(width)}
	ys := []float64{0, float64(height)}
	for _, r := range areas {
		xs = append(xs, clamp(r.X, 0, float64(width)), clamp(r.X+r.W, 0, float64(width)))
		ys = append(ys, clamp(r.Y, 0, float64(height)), clamp(r.Y+r.H, 0, float64(height)))
	}
	slices.Sort(xs)
	slices.Sort(ys)
	xs, ys = slices.Compact(xs), slices.Compact(ys)
	var p ui.Path
	for j := 0; j+1 < len(ys); j++ {
		for i := 0; i+1 < len(xs); i++ {
			cx, cy := (xs[i]+xs[i+1])/2, (ys[j]+ys[j+1])/2
			covered := false
			for _, r := range areas {
				if cx > r.X && cx < r.X+r.W && cy > r.Y && cy < r.Y+r.H {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			p.MoveTo(f(xs[i]), f(ys[j])).LineTo(f(xs[i+1]), f(ys[j])).
				LineTo(f(xs[i+1]), f(ys[j+1])).LineTo(f(xs[i]), f(ys[j+1])).Close()
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
