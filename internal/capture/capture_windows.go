//go:build windows

package capture

import (
	"context"
	"errors"
	"github.com/wildalley/screenpin/internal/brand"
	"image"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/wildalley/screenpin/internal/imagelimit"
)

var (
	screenUser32       = syscall.NewLazyDLL("user32.dll")
	screenGDI32        = syscall.NewLazyDLL("gdi32.dll")
	getDC              = screenUser32.NewProc("GetDC")
	releaseDC          = screenUser32.NewProc("ReleaseDC")
	getSystemMetrics   = screenUser32.NewProc("GetSystemMetrics")
	setThreadDPI       = screenUser32.NewProc("SetThreadDpiAwarenessContext")
	setDisplayAffinity = screenUser32.NewProc("SetWindowDisplayAffinity")
	createCompatibleDC = screenGDI32.NewProc("CreateCompatibleDC")
	createDIBSection   = screenGDI32.NewProc("CreateDIBSection")
	selectObject       = screenGDI32.NewProc("SelectObject")
	deleteObject       = screenGDI32.NewProc("DeleteObject")
	deleteDC           = screenGDI32.NewProc("DeleteDC")
	bitBlt             = screenGDI32.NewProc("BitBlt")
	gdiFlush           = screenGDI32.NewProc("GdiFlush")
)

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

// Windows callers must use DesktopPixels plus the app's region picker.
func System(readImage func() []byte) ([]byte, error) {
	return SystemContext(context.Background(), readImage)
}
func SystemContext(ctx context.Context, _ func() []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("Windows 截图使用 " + brand.Name + " 自有选区")
}

// DesktopPixels captures the virtual desktop into memory. Bounds retain
// physical coordinates, including monitors left/above the primary.
// It never invokes Snipping Tool, touches the clipboard or creates a file.
func DesktopPixels(ctx context.Context) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := setThreadDPI.Find(); err == nil {
		previous, _, _ := setThreadDPI.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2 (-4)
		if previous != 0 {
			defer setThreadDPI.Call(previous)
		}
	}
	metric := func(id uintptr) int { v, _, _ := getSystemMetrics.Call(id); return int(int32(v)) }
	x, y, w, h := metric(76), metric(77), metric(78), metric(79)
	if imagelimit.Dimensions(w, h) != nil {
		return nil, errors.New("桌面尺寸超出截图支持范围")
	}
	dc, _, _ := getDC.Call(0)
	if dc == 0 {
		return nil, errors.New("无法读取 Windows 桌面")
	}
	defer releaseDC.Call(0, dc)
	memory, _, _ := createCompatibleDC.Call(dc)
	if memory == 0 {
		return nil, errors.New("无法创建截图缓冲区")
	}
	defer deleteDC.Call(memory)
	info := bitmapInfoHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bitmap, _, _ := createDIBSection.Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return nil, errors.New("无法分配截图像素")
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(memory, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return nil, errors.New("无法选择截图缓冲区")
	}
	defer selectObject.Call(memory, old)
	ok, _, _ := bitBlt.Call(memory, 0, 0, uintptr(w), uintptr(h), dc, uintptr(x), uintptr(y), 0x40CC0020) // SRCCOPY | CAPTUREBLT
	if ok == 0 {
		return nil, errors.New("无法截取 Windows 桌面")
	}
	gdiFlush.Call()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(x, y, x+w, y+h))
	src := unsafe.Slice((*byte)(bits), w*h*4)
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return img, ctx.Err()
}

// ExcludeWindow prevents the floating controller from entering WGC frames.
// Before Windows 10 2004 a window cannot be excluded; keep it hidden there.
func ExcludeWindow(handle uintptr) bool {
	if handle == 0 {
		return false
	}
	var version struct {
		Size, Major, Minor, Build, Platform uint32
		ServicePack                         [128]uint16
	}
	version.Size = uint32(unsafe.Sizeof(version))
	r, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&version)))
	if r != 0 || version.Build < 19041 {
		return false
	}
	r, _, _ = setDisplayAffinity.Call(handle, 0x11)
	return r != 0
}
