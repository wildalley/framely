package editor

import (
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

const maxImageSize = 220 << 20

// Picture is a validated image ready to open. It has no file, project or
// persistence lifecycle. Encoded input is released after decoding.
type Picture struct {
	image  doc.Image
	pixels *image.RGBA
	mosaic *image.RGBA
	title  string
}

func DecodePicture(data []byte, name string) (Picture, error) {
	if len(data) > maxImageSize {
		return Picture{}, errors.New("图片文件过大")
	}
	im, err := doc.DecodeImage(data, name)
	if err != nil {
		return Picture{}, err
	}
	px, err := capture.Decode(data)
	if err != nil {
		return Picture{}, err
	}
	if px.Bounds().Dx() != im.Width || px.Bounds().Dy() != im.Height {
		return Picture{}, errors.New("图片尺寸不一致")
	}
	im.Data = nil
	title := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if title == "" {
		title = "截图"
	}
	return Picture{image: im, pixels: px, mosaic: render.Pixelate(px), title: title}, nil
}

// ReadPicture accepts image bytes only. A corrupt image or .screenpin project
// never reaches NewWindow, and opening a file cannot replace another image.
func ReadPicture(path string) (Picture, error) {
	f, err := os.Open(path)
	if err != nil {
		return Picture{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageSize+1))
	if err != nil {
		return Picture{}, err
	}
	return DecodePicture(data, filepath.Base(path))
}
