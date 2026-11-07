package render

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// Preview bounds GPU texture sizes while keeping annotation coordinates and
// export pixels at original resolution, including very tall screenshots.
func Preview(src *image.RGBA, maxSide int) *image.RGBA {
	b := src.Bounds()
	if max(b.Dx(), b.Dy()) <= maxSide {
		return src
	}
	scale := float64(maxSide) / float64(max(b.Dx(), b.Dy()))
	out := image.NewRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), src, b, draw.Src, nil)
	return out
}
