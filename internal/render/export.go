package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/egoist/mygo/ui"
	"github.com/gen2brain/webp"
	"github.com/wildalley/screenpin/internal/doc"
)

// Format is a format the editor exports to.
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
	WebP Format = "webp"
)

// Formats lists the export formats in the order the menu shows them.
var Formats = []Format{PNG, JPEG, WebP}

// Label is the format's name as the menu shows it.
func (f Format) Label() string {
	switch f {
	case JPEG:
		return "JPEG"
	case WebP:
		return "WebP"
	}
	return "PNG"
}

// Extension is the file name extension of the format.
func (f Format) Extension() string {
	switch f {
	case JPEG:
		return "jpg"
	case WebP:
		return "webp"
	}
	return "png"
}

// Lossy reports whether the format takes a quality.
func (f Format) Lossy() bool { return f == JPEG || f == WebP }

// ErrMosaicUnavailable stops an export that would show what a mosaic hides.
var ErrMosaicUnavailable = errors.New("马赛克处理失败，已阻止导出以保护原图")

// Flatten draws the annotations over src at its full size. It builds bitmaps
// of its own, so it can run on any goroutine while the window draws.
func Flatten(src, mosaic *image.RGBA, list []doc.Annotation) (*image.RGBA, error) {
	return FlattenWithCorners(src, mosaic, list, 0)
}

// FlattenWithCorners clips both pixels and annotations, preserving alpha for
// PNG/WebP. JPEG encoding composites those transparent corners on white.
func FlattenWithCorners(src, mosaic *image.RGBA, list []doc.Annotation, ratio float64) (*image.RGBA, error) {
	return FlattenRegion(src, mosaic, list, ratio, src.Bounds())
}

// FlattenRegion renders only the requested pixels while retaining the full
// scene's annotation and corner coordinates, including privacy masks.
func FlattenRegion(src, mosaic *image.RGBA, list []doc.Annotation, ratio float64, region image.Rectangle) (*image.RGBA, error) {
	b := src.Bounds()
	region = region.Intersect(b)
	if region.Empty() {
		return nil, errors.New("识别选区为空")
	}
	for i := range list {
		if list[i].Type == doc.ToolMosaic && mosaic == nil {
			return nil, ErrMosaicUnavailable
		}
	}
	ratio = doc.ClampCornerRatio(ratio)
	if len(list) == 0 && ratio == 0 {
		if region == b {
			return src, nil
		}
		out := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
		draw.Draw(out, out.Bounds(), src, region.Min, draw.Src)
		return out, nil
	}
	s := &Scene{Image: ui.NewBitmap(src), Width: b.Dx(), Height: b.Dy(), Annotations: list, CornerRatio: ratio}
	if mosaic != nil {
		s.Mosaic = ui.NewBitmap(mosaic).Pixelated()
	}
	out := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Size(float32(region.Dx()), float32(region.Dy())).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Transform(ui.Translation(r.X-float32(region.Min.X-b.Min.X), r.Y-float32(region.Min.Y-b.Min.Y)), 0, 0, func() { Draw(p, s) })
		})
	}, region.Dx(), region.Dy(), 1)
	if out == nil || out.Bounds().Dx() != region.Dx() || out.Bounds().Dy() != region.Dy() {
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
	if img == nil || img.Bounds().Empty() {
		return nil, errors.New("无法编码空图片")
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
	case WebP:
		// libwebp expects straight alpha and the portable backend expects
		// tightly packed rows. Normalize both, including cropped subimages.
		straight := image.NewNRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(straight, straight.Bounds(), img, img.Bounds().Min, draw.Src)
		q := max(1, min(100, int(quality*100+0.5)))
		err := webp.Encode(out, straight, webp.Options{Quality: q, Method: webp.DefaultMethod})
		// The native backend may replace a writer error with ErrEncode.
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, cancelErr
		}
		if err != nil {
			return nil, err
		}
	case PNG:
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(out, img); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("不支持的图片格式: %q", format)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
