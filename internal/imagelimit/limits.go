// Package imagelimit defines shared admission limits for decoded pictures.
package imagelimit

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	_ "golang.org/x/image/webp"
)

const (
	MaxSide            = 32768
	MaxPixels          = 32_000_000
	MaxEncodedBytes    = 220 << 20
	PictureBudgetBytes = 512 << 20
	HistoryBytes       = 32 << 20
)

var ErrDimensions = errors.New("图片尺寸超出支持范围（最多 3200 万像素）")
var ErrEncodedSize = errors.New("图片文件过大")

func Dimensions(w, h int) error {
	if w <= 0 || h <= 0 || w > MaxSide || h > MaxSide || int64(w)*int64(h) > MaxPixels {
		return ErrDimensions
	}
	return nil
}

// Config rejects compressed image bombs before allocating decoded pixels.
func Config(data []byte) (image.Config, string, error) {
	if len(data) > MaxEncodedBytes {
		return image.Config{}, "", ErrEncodedSize
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return cfg, format, err
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return cfg, format, errors.New("不支持的图片格式")
	}
	return cfg, format, Dimensions(cfg.Width, cfg.Height)
}

// ReadFile bounds both the stat size and the actual read (files may grow).
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("图片来源必须是普通文件")
	}
	if info.Size() > MaxEncodedBytes {
		return nil, ErrEncodedSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxEncodedBytes {
		return nil, ErrEncodedSize
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxEncodedBytes+1))
	if err == nil && len(data) > MaxEncodedBytes {
		err = ErrEncodedSize
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}
