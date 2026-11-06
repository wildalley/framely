// Package capture takes screenshots with what each platform provides: the
// screenshot portal on Linux, screencapture on macOS and the Snipping Tool
// on Windows.
package capture

import (
	"bytes"
	"errors"
	"image"
	"image/png"

	"github.com/wildalley/screenpin/internal/portal"
	"github.com/wildalley/screenpin/internal/render"
)

// ErrCancelled is returned when the user backed out of the capture. It is
// not a failure: callers put things back as they were and say nothing.
var ErrCancelled = errors.New("截图已取消")

// AppID is the desktop entry the Linux portals know ScreenPin by. It must
// match the installed io.screenpin.desktop.desktop, as it did before, so the
// permissions already granted carry over.
const AppID = portal.AppID

// Crop cuts r out of img and encodes it as PNG.
func Crop(img *image.RGBA, r image.Rectangle) ([]byte, error) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return nil, errors.New("截图选区超出图片范围")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img.SubImage(r)); err != nil {
		return nil, errors.New("无法编码选区: " + err.Error())
	}
	return buf.Bytes(), nil
}

// Decode decodes a screenshot to RGBA pixels.
func Decode(data []byte) (*image.RGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("无法读取截图: " + err.Error())
	}
	return render.ToRGBA(img), nil
}

// Access is whether ScreenPin may take silent screenshots.
type Access string

const (
	AccessGranted     Access = "granted"
	AccessDenied      Access = "denied"
	AccessUnknown     Access = "unknown" // never asked
	AccessUnsupported Access = "unsupported"
)
