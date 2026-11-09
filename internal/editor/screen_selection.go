package editor

import (
	"errors"
	"image"
	"math"
	"runtime"

	"github.com/egoist/mygo"
)

// ScreenSelection remembers where a fresh screenshot was taken. Imported,
// pasted and stitched images have no live desktop region to reuse.
type ScreenSelection struct {
	Display mygo.Rectangle
	Region  image.Rectangle // display-relative pixels
	Size    image.Point     // captured display pixel size
}

func ScreenSelectionFromDesktop(region image.Rectangle, target *mygo.Rectangle, displays []mygo.Display, fullBounds image.Rectangle) (*ScreenSelection, error) {
	if target == nil || fullBounds.Empty() {
		return nil, errors.New("截图未记录显示器位置")
	}
	crops := layoutScreens(displays, fullBounds.Dx(), fullBounds.Dy())
	for i, display := range displays {
		if display.Bounds != *target {
			continue
		}
		crop := crops[i].Add(fullBounds.Min)
		if runtime.GOOS == "windows" {
			crop = physicalDisplay(display).Intersect(fullBounds)
		}
		if crop.Empty() || region.Empty() || !region.In(crop) {
			return nil, errors.New("截图选区超出了显示器")
		}
		return &ScreenSelection{Display: *target, Region: region.Sub(crop.Min), Size: crop.Size()}, nil
	}
	return nil, errors.New("截图所在的显示器已断开")
}

func (s ScreenSelection) ForStream(size image.Point) (image.Rectangle, error) {
	if s.Size.X <= 0 || s.Size.Y <= 0 || size.X <= 0 || size.Y <= 0 || s.Region.Empty() || !s.Region.In(image.Rectangle{Max: s.Size}) {
		return image.Rectangle{}, errors.New("截图未记录有效选区")
	}
	sx, sy := float64(size.X)/float64(s.Size.X), float64(size.Y)/float64(s.Size.Y)
	return image.Rect(
		int(math.Floor(float64(s.Region.Min.X)*sx)),
		int(math.Floor(float64(s.Region.Min.Y)*sy)),
		int(math.Ceil(float64(s.Region.Max.X)*sx)),
		int(math.Ceil(float64(s.Region.Max.Y)*sy)),
	).Intersect(image.Rectangle{Max: size}), nil
}

func (p Picture) WithScreenSelection(selection *ScreenSelection) Picture {
	if selection != nil {
		copy := *selection
		p.selection = &copy
	}
	return p
}

func (a *App) ScreenSelection() *ScreenSelection {
	if a.closed || a.selection == nil {
		return nil
	}
	copy := *a.selection
	return &copy
}
