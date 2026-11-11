package editor

import (
	"errors"
	"image"
	"path/filepath"
	"strings"

	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/doc"
	"github.com/wildalley/framely/internal/imagelimit"
	"github.com/wildalley/framely/internal/render"
)

const maxImageSize = imagelimit.MaxEncodedBytes

// Picture is a validated image ready to open. It has no file, project or
// persistence lifecycle. Encoded input is released after decoding.
type Picture struct {
	image     doc.Image
	pixels    *image.RGBA
	mosaic    *image.RGBA
	title     string
	selection *ScreenSelection
	pick      Pick
	lease     *imagelimit.Lease
}

// WithPick records what the region picker chose to do with this picture.
func (p Picture) WithPick(choice Pick) Picture { p.pick = choice; return p }

// Pick is what the region picker chose; PickEdit for other pictures.
func (p Picture) Pick() Pick { return p.pick }

// Selection is where on screen a fresh screenshot was taken, or nil.
func (p Picture) Selection() *ScreenSelection {
	if p.selection == nil {
		return nil
	}
	copy := *p.selection
	return &copy
}

// Close releases this ownership handle; call it after a synchronous handoff.
func (p Picture) Close() { p.lease.Release() }

// Retain keeps a picture accounted for during an independent background task.
func (p Picture) Retain() Picture { p.lease = p.lease.Retain(); return p }

// OCRPixels transfers the already-cropped capture to a direct text task.
// The caller treats the returned pixels as immutable.
func (p Picture) OCRPixels() *image.RGBA { return p.pixels }

// PictureSlot reserves the upper bound of an image before expensive work.
// Long capture reserves its stitch limit, then shrinks to its actual output.
// A slot prepares one picture; Close is required even after FromPixels.
type PictureSlot struct {
	lease  *imagelimit.Lease
	pixels int64
	used   bool
}

func ReservePicture(pixels int64) (*PictureSlot, error) {
	if pixels <= 0 || pixels > imagelimit.MaxPixels {
		return nil, imagelimit.ErrDimensions
	}
	lease, err := imagelimit.Pictures.Acquire(imagelimit.PictureBytes(int(pixels), 1))
	if err != nil {
		return nil, err
	}
	return &PictureSlot{lease: lease, pixels: pixels}, nil
}

func (s *PictureSlot) Close() {
	if s != nil {
		s.lease.Release()
	}
}

// PictureFromPixels takes ownership of completed native capture pixels without
// a PNG encode/decode round trip and its duplicate long-image allocations.
func PictureFromPixels(pixels *image.RGBA, title string) (Picture, error) {
	if pixels == nil || imagelimit.Dimensions(pixels.Rect.Dx(), pixels.Rect.Dy()) != nil {
		return Picture{}, errors.New("图片尺寸超出支持范围")
	}
	slot, err := ReservePicture(int64(pixels.Rect.Dx()) * int64(pixels.Rect.Dy()))
	if err != nil {
		return Picture{}, err
	}
	defer slot.Close()
	return slot.FromPixels(pixels, title)
}

func (s *PictureSlot) FromPixels(pixels *image.RGBA, title string) (Picture, error) {
	if pixels == nil || imagelimit.Dimensions(pixels.Rect.Dx(), pixels.Rect.Dy()) != nil {
		return Picture{}, imagelimit.ErrDimensions
	}
	if s == nil || s.used || int64(pixels.Rect.Dx())*int64(pixels.Rect.Dy()) > s.pixels || !s.lease.ReduceTo(imagelimit.PictureBytes(pixels.Rect.Dx(), pixels.Rect.Dy())) {
		return Picture{}, imagelimit.ErrBudget
	}
	s.used = true
	lease := s.lease.Retain()
	var err error
	// Detach a subimage's larger backing allocation so a small selection cannot
	// keep an unaccounted full desktop alive for the lifetime of the window.
	if pixels.Rect.Min != (image.Point{}) || pixels.Stride != pixels.Rect.Dx()*4 || cap(pixels.Pix) != pixels.Rect.Dx()*pixels.Rect.Dy()*4 {
		pixels, err = capture.CropPixels(pixels, pixels.Bounds())
		if err != nil {
			lease.Release()
			return Picture{}, err
		}
	}
	return Picture{image: doc.Image{Width: pixels.Rect.Dx(), Height: pixels.Rect.Dy(), Mime: "image/png", Name: title + ".png"}, pixels: pixels, mosaic: render.Pixelate(pixels), title: title, lease: lease}, nil
}

func DecodePicture(data []byte, name string) (Picture, error) {
	if len(data) > maxImageSize {
		return Picture{}, errors.New("图片文件过大")
	}
	im, err := doc.DecodeImage(data, name)
	if err != nil {
		return Picture{}, err
	}
	lease, err := imagelimit.Pictures.Acquire(imagelimit.PictureBytes(im.Width, im.Height))
	if err != nil {
		return Picture{}, err
	}
	px, err := capture.Decode(data)
	if err != nil {
		lease.Release()
		return Picture{}, err
	}
	if px.Bounds().Dx() != im.Width || px.Bounds().Dy() != im.Height {
		lease.Release()
		return Picture{}, errors.New("图片尺寸不一致")
	}
	im.Data = nil
	title := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if title == "" {
		title = "截图"
	}
	return Picture{image: im, pixels: px, mosaic: render.Pixelate(px), title: title, lease: lease}, nil
}

// ReadPicture accepts image bytes only. A corrupt image or .screenpin project
// never reaches NewWindow, and opening a file cannot replace another image.
func ReadPicture(path string) (Picture, error) {
	data, err := imagelimit.ReadFile(path)
	if err != nil {
		return Picture{}, err
	}
	return DecodePicture(data, filepath.Base(path))
}

// PNG encodes the picture as taken, for copying a region without opening
// a window.
func (p Picture) PNG() ([]byte, error) {
	if p.pixels == nil {
		return nil, errors.New("没有图片")
	}
	return render.Encode(p.pixels, render.PNG, 1)
}
