package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
)

var ErrAutomaticUnavailable = errors.New("当前桌面未提供指针控制接口，请使用手动滚动模式")

func ScrollingSupported() bool { return runtime.GOOS == "linux" || runtime.GOOS == "windows" }

type StreamOptions struct {
	Automatic bool
	Monitor   int
}
type Monitor struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	UnderMouse bool   `json:"underMouse"`
}
type Stream interface {
	Next(context.Context) (*image.RGBA, error)
	Scroll(context.Context, image.Point) error
	Close()
}

type processStream struct {
	frames        chan *image.RGBA
	done          chan struct{}
	err           error
	cancel        context.CancelFunc
	closeOnce     sync.Once
	closed        atomic.Bool
	cleanup       func()
	scroll        func(context.Context, image.Point) error
	control       chan controlReply
	width, height atomic.Int32
	log           *streamLog
}

func startStream(parent context.Context, cmd *exec.Cmd, cancel context.CancelFunc, cleanup func()) (*processStream, error) {
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanup()
		return nil, err
	}
	var log streamLog
	cmd.Stderr = &log
	if err = cmd.Start(); err != nil {
		cancel()
		cleanup()
		return nil, err
	}
	s := &processStream{frames: make(chan *image.RGBA, 1), control: make(chan controlReply, 4), done: make(chan struct{}), cancel: cancel, cleanup: cleanup}
	s.log = &log
	go func() {
		defer close(s.done)
		defer out.Close()
		for {
			frame, reply, readErr := readPacket(out)
			if readErr != nil {
				parentErr := parent.Err()
				cancel() // malformed output must not leave the helper alive
				waitErr := cmd.Wait()
				if parentErr != nil {
					s.err = parentErr
				} else {
					s.err = fmt.Errorf("画面采集已结束: %w %v\n%s", readErr, waitErr, log.String())
				}
				return
			}
			if reply != nil {
				select {
				case s.control <- *reply:
				default:
				}
				continue
			}
			s.width.Store(int32(frame.Bounds().Dx()))
			s.height.Store(int32(frame.Bounds().Dy()))
			select {
			case s.frames <- frame:
			default:
				select {
				case <-s.frames:
				default:
				}
				select {
				case s.frames <- frame:
				default:
				}
			}
		}
	}()
	return s, nil
}

func (s *processStream) Next(ctx context.Context) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, context.Canceled
	}
	select {
	case img := <-s.frames:
		return img, nil
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case img := <-s.frames:
		return img, nil
	case <-s.done:
		select {
		case img := <-s.frames:
			return img, nil
		default:
			return nil, s.err
		}
	}
}
func (s *processStream) Scroll(ctx context.Context, p image.Point) error {
	if s.scroll == nil {
		return ErrAutomaticUnavailable
	}
	return s.scroll(ctx, p)
}
func (s *processStream) Close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.cancel()
		<-s.done
		// The stream can remain referenced while the user reviews a finished
		// capture. A queued desktop frame can otherwise retain up to 128 MB.
		select {
		case <-s.frames:
		default:
		}
		s.cleanup()
	})
}

// Native workers stream bounded, tightly packed pixel frames. The reader
// drains continuously and only retains the latest frame, not an ever-growing
// queue when selection, stitching or the UI takes longer than capture.
type controlReply struct {
	id  uint32
	err error
}

func readFrame(r io.Reader) (*image.RGBA, error) { img, _, err := readPacket(r); return img, err }

func readPacket(r io.Reader) (*image.RGBA, *controlReply, error) {
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	if string(header[:4]) == "SPC1" {
		n := binary.LittleEndian.Uint32(header[4:8])
		code := binary.LittleEndian.Uint32(header[8:12])
		id := binary.LittleEndian.Uint32(header[12:])
		if n > 4096 {
			return nil, nil, errors.New("采集组件控制响应过大")
		}
		message := make([]byte, n)
		if _, err := io.ReadFull(r, message); err != nil {
			return nil, nil, err
		}
		reply := &controlReply{id: id}
		if code != 0 {
			reply.err = errors.New(string(message))
		}
		return nil, reply, nil
	}
	if string(header[:4]) != "SPF1" {
		return nil, nil, errors.New("采集组件帧头无效")
	}
	w, h, format := binary.LittleEndian.Uint32(header[4:8]), binary.LittleEndian.Uint32(header[8:12]), binary.LittleEndian.Uint32(header[12:])
	if w == 0 || h == 0 || w > 16384 || h > 16384 || uint64(w)*uint64(h) > 32_000_000 || format < 1 || format > 4 {
		return nil, nil, errors.New("采集组件图片尺寸或像素格式无效")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if _, err := io.ReadFull(r, img.Pix); err != nil {
		return nil, nil, err
	}
	for i := 0; i < len(img.Pix); i += 4 {
		if format == 1 || format == 3 {
			img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		}
		// The capture is the visible composited image, not straight-alpha
		// document pixels. Window shadows must not reveal hidden RGB in OCR.
		img.Pix[i+3] = 255
	}
	return img, nil, nil
}

type streamLog struct{ bytes.Buffer }

func (b *streamLog) Write(p []byte) (int, error) {
	n := len(p)
	if room := (16 << 10) - b.Len(); room > 0 {
		_, _ = b.Buffer.Write(p[:min(room, len(p))])
	}
	return n, nil
}
