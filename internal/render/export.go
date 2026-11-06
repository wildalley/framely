package render

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
)

// Format is a format the editor exports to.
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
)

// Formats lists the export formats in the order the menu shows them.
var Formats = []Format{PNG, JPEG}

// Label is the format's name as the menu shows it.
func (f Format) Label() string {
	if f == JPEG {
		return "JPEG"
	}
	return "PNG"
}

// Extension is the file name extension of the format.
func (f Format) Extension() string {
	if f == JPEG {
		return "jpg"
	}
	return "png"
}

// Lossy reports whether the format takes a quality.
func (f Format) Lossy() bool { return f == JPEG }

// ErrMosaicUnavailable stops an export that would show what a mosaic hides.
var ErrMosaicUnavailable = errors.New("马赛克处理失败，已阻止导出以保护原图")

// Flatten draws the annotations over src at its full size. It builds bitmaps
// of its own, so it can run on any goroutine while the window draws.
func Flatten(src, mosaic *image.RGBA, list []doc.Annotation) (*image.RGBA, error) {
	b := src.Bounds()
	for i := range list {
		if list[i].Type == doc.ToolMosaic && mosaic == nil {
			return nil, ErrMosaicUnavailable
		}
	}
	if len(list) == 0 {
		return src, nil
	}
	s := &Scene{Image: ui.NewBitmap(src), Width: b.Dx(), Height: b.Dy(), Annotations: list}
	if mosaic != nil {
		s.Mosaic = ui.NewBitmap(mosaic).Pixelated()
	}
	out := ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(float32(b.Dx()), float32(b.Dy())).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Transform(ui.Translation(r.X, r.Y), 0, 0, func() { Draw(p, s) })
		})
	}, b.Dx(), b.Dy(), 1)
	if out == nil || out.Bounds().Dx() != b.Dx() || out.Bounds().Dy() != b.Dy() {
		return nil, errors.New("无法渲染导出图片")
	}
	return out, nil
}

// Encode encodes img in format; quality, from 0 to 1, applies to lossy ones.
func Encode(img *image.RGBA, format Format, quality float64) ([]byte, error) {
	return EncodeContext(context.Background(), img, format, quality)
}

// EncodeContext stops producing output when its image window is closed.
func EncodeContext(ctx context.Context, img *image.RGBA, format Format, quality float64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	out := contextWriter{ctx, &buf}
	switch format {
	case JPEG:
		// JPEG has no alpha channel: put the image on white, as the web
		// editor did, so transparent pixels do not turn black.
		flat := image.NewRGBA(img.Bounds())
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		q := int(quality*100 + 0.5)
		if err := jpeg.Encode(out, flat, &jpeg.Options{Quality: max(1, min(100, q))}); err != nil {
			return nil, err
		}
	default:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(out, img); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

type contextWriter struct {
	ctx context.Context
	buf *bytes.Buffer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.buf.Write(p)
}
