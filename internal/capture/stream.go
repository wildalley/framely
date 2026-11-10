package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wildalley/screenpin/internal/imagelimit"
)

var ErrAutomaticUnavailable = errors.New("当前桌面未提供指针控制接口，请使用手动滚动模式")

func ScrollingSupported() bool { return runtime.GOOS == "linux" || runtime.GOOS == "windows" }

type StreamOptions struct {
	Automatic     bool
	Monitor       int
	DisplayBounds image.Rectangle
	Recording     *RecordingOptions
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
	frames        chan streamFrame
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
	displayBounds image.Rectangle
	sourceType    uint32
	protocol      atomic.Uint32
	minGeneration atomic.Uint32
	input         io.WriteCloser
	commandMu     sync.Mutex
	commandID     uint32
	statusMu      sync.Mutex
	status        RecordingStatus
}

type streamFrame struct {
	pixels     *image.RGBA
	source     image.Point
	generation uint32
	protocol   uint32
	status     *RecordingStatus
}

// SetStreamRegion switches native capture to tightly packed region pixels.
// Stream coordinates and automatic pointer input remain in source pixels.
func SetStreamRegion(ctx context.Context, source Stream, region image.Rectangle, size image.Point) error {
	if region.Empty() || !region.In(image.Rectangle{Max: size}) {
		return errors.New("采集选区超出来源")
	}
	s, ok := source.(*processStream)
	if !ok {
		// In-memory streams may still supply full frames.
		return nil
	}
	if s.protocol.Load() != 2 || s.input == nil {
		return errors.New("采集组件版本不匹配，请重新构建或安装完整组件")
	}
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	id, err := s.sendCommand(ctx, "crop", region.Min.X, region.Min.Y, region.Dx(), region.Dy(), size.X, size.Y)
	if err == nil {
		s.minGeneration.Store(id)
	}
	return err
}

// Caller serializes command writes and replies with commandMu.
func (s *processStream) sendCommand(ctx context.Context, name string, args ...int) (uint32, error) {
	timeout := 2 * time.Second
	if name == "record" || name == "finish" {
		timeout = 15 * time.Second
	}
	ctx, done := context.WithTimeout(ctx, timeout)
	defer done()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.commandID++
	id := s.commandID
	var command bytes.Buffer
	fmt.Fprintf(&command, "%s %d", name, id)
	for _, arg := range args {
		fmt.Fprintf(&command, " %d", arg)
	}
	command.WriteByte('\n')
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	if _, err := s.input.Write(command.Bytes()); err != nil {
		return id, err
	}
	for {
		select {
		case <-ctx.Done():
			return id, ctx.Err()
		case <-s.done:
			if s.err != nil {
				return id, s.err
			}
			return id, io.EOF
		case reply := <-s.control:
			if reply.id == id {
				return id, reply.err
			}
		}
	}
}

// StreamSourceType uses portal source types: 1 is a monitor, 2 a window.
func StreamSourceType(source Stream) uint32 {
	if s, ok := source.(*processStream); ok {
		return s.sourceType
	}
	return 0
}

// StreamDisplayBounds is available when the portal identifies a whole display
// in desktop logical coordinates. Window-only streams use the image picker.
func StreamDisplayBounds(source Stream) (image.Rectangle, bool) {
	s, ok := source.(*processStream)
	if !ok {
		return image.Rectangle{}, false
	}
	return s.displayBounds, !s.displayBounds.Empty()
}

// Read diagnostics only after Close has stopped the stderr writer.
func StreamDiagnostic(source Stream, err error) error {
	if s, ok := source.(*processStream); ok && s.closed.Load() && s.log != nil {
		select {
		case <-s.done:
		default:
			return err
		}
		if s.log.Len() > 0 {
			return fmt.Errorf("%w\n%s", err, s.log.String())
		}
	}
	return err
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
		_ = out.Close()
		cancel()
		cleanup()
		return nil, err
	}
	s := &processStream{frames: make(chan streamFrame, 1), control: make(chan controlReply, 4), done: make(chan struct{}), cancel: cancel, cleanup: cleanup}
	s.log = &log
	go func() {
		defer close(s.done)
		defer out.Close()
		var original image.Point
		for {
			frame, reply, readErr := readStreamPacket(out)
			if readErr == nil && frame.pixels != nil {
				if original == (image.Point{}) {
					original = frame.source
				}
				if frame.source != original {
					readErr = errors.New("采集来源尺寸已变化，请重新开始长截图")
				}
			}
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
			if frame.status != nil {
				s.statusMu.Lock()
				s.status = *frame.status
				s.statusMu.Unlock()
				continue
			}
			s.width.Store(int32(frame.source.X))
			s.height.Store(int32(frame.source.Y))
			s.protocol.Store(frame.protocol)
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
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.closed.Load() {
			return nil, context.Canceled
		}
		select {
		case img := <-s.frames:
			if img.generation >= s.minGeneration.Load() {
				return img.pixels, nil
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case img := <-s.frames:
			if img.generation >= s.minGeneration.Load() {
				return img.pixels, nil
			}
		case <-s.done:
			select {
			case img := <-s.frames:
				if img.generation >= s.minGeneration.Load() {
					return img.pixels, nil
				}
				return nil, s.err
			default:
				return nil, s.err
			}
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
	f, reply, err := readStreamPacket(r)
	return f.pixels, reply, err
}

func readStreamPacket(r io.Reader) (streamFrame, *controlReply, error) {
	var frame streamFrame
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frame, nil, err
	}
	if string(header[:4]) == "SPC1" {
		n := binary.LittleEndian.Uint32(header[4:8])
		code := binary.LittleEndian.Uint32(header[8:12])
		id := binary.LittleEndian.Uint32(header[12:])
		if n > 4096 {
			return frame, nil, errors.New("采集组件控制响应过大")
		}
		message := make([]byte, n)
		if _, err := io.ReadFull(r, message); err != nil {
			return frame, nil, err
		}
		reply := &controlReply{id: id}
		if code != 0 {
			reply.err = errors.New(string(message))
		}
		return frame, reply, nil
	}
	if string(header[:4]) == "SPR1" {
		n := binary.LittleEndian.Uint32(header[4:8])
		if n > 4096 {
			return frame, nil, errors.New("录屏状态过大")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return frame, nil, err
		}
		var status RecordingStatus
		if err := json.Unmarshal(data, &status); err != nil {
			return frame, nil, err
		}
		if status.ElapsedMS < 0 || status.Frames < 0 || status.Captured < 0 || status.Repeated < 0 || status.Repeated > status.Frames || status.Missed < 0 || status.Width < 0 || status.Height < 0 || status.Width > 3840 || status.Height > 3840 || (status.FPS != 30 && status.FPS != 60) {
			return frame, nil, errors.New("录屏状态无效")
		}
		frame.status = &status
		return frame, nil, nil
	}
	magic := string(header[:4])
	if magic != "SPF1" && magic != "SPF2" {
		return frame, nil, errors.New("采集组件帧头无效")
	}
	w, h, format := binary.LittleEndian.Uint32(header[4:8]), binary.LittleEndian.Uint32(header[8:12]), binary.LittleEndian.Uint32(header[12:])
	if imagelimit.Dimensions(int(w), int(h)) != nil || w > 16384 || h > 16384 || format < 1 || format > 4 {
		return frame, nil, errors.New("采集组件图片尺寸或像素格式无效")
	}
	region := image.Rect(0, 0, int(w), int(h))
	frame.source, frame.protocol = region.Size(), 1
	if magic == "SPF2" {
		var meta [20]byte
		if _, err := io.ReadFull(r, meta[:]); err != nil {
			return frame, nil, err
		}
		x, y := binary.LittleEndian.Uint32(meta[:4]), binary.LittleEndian.Uint32(meta[4:8])
		sw, sh := binary.LittleEndian.Uint32(meta[8:12]), binary.LittleEndian.Uint32(meta[12:16])
		if imagelimit.Dimensions(int(sw), int(sh)) != nil || sw > 16384 || sh > 16384 || x > sw || y > sh || w > sw-x || h > sh-y {
			return frame, nil, errors.New("采集组件选区或来源尺寸无效")
		}
		region = region.Add(image.Pt(int(x), int(y)))
		frame.source, frame.protocol, frame.generation = image.Pt(int(sw), int(sh)), 2, binary.LittleEndian.Uint32(meta[16:])
	}
	img := image.NewRGBA(region)
	if _, err := io.ReadFull(r, img.Pix); err != nil {
		return frame, nil, err
	}
	for i := 0; i < len(img.Pix); i += 4 {
		if format == 1 || format == 3 {
			img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		}
		// The capture is the visible composited image, not straight-alpha
		// document pixels. Window shadows must not reveal hidden RGB in OCR.
		img.Pix[i+3] = 255
	}
	frame.pixels = img
	return frame, nil, nil
}

type streamLog struct{ bytes.Buffer }

func (b *streamLog) Write(p []byte) (int, error) {
	n := len(p)
	if room := (16 << 10) - b.Len(); room > 0 {
		_, _ = b.Buffer.Write(p[:min(room, len(p))])
	}
	return n, nil
}
