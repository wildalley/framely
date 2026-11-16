package scrollshot

import (
	"context"
	"errors"
	"image"
	"sync"
	"time"
)

// Source supplies frames and optional automatic pointer input. It has no
// dependency on a native window, portal implementation or desktop toolkit.
type Source interface {
	Next(context.Context) (*image.RGBA, error)
	Scroll(context.Context, image.Point) error
	Close()
}

type Command struct {
	Kind      string
	Automatic bool
}

const PreviewWidth, PreviewHeight = 720, 820

// Progress is a detached snapshot safe to queue on another goroutine.
type Progress struct {
	Preview                          *image.RGBA
	Height                           int
	CanUndo, Paused, Automatic, Show bool
	SourceEnded                      bool
	Notice                           string
}

// Session serializes controller updates, controls and source lifetime on the
// capture goroutine. The caller retains Controller to flatten/release the
// stitch after Run; callbacks receive only snapshots, never live state.
type Session struct {
	Controller  *Controller
	Region      image.Rectangle
	SourceSize  image.Point
	Commands    <-chan Command
	Progress    func(Progress)
	sourceEnded bool
	closeSource func()
}

type commandAction uint8

const (
	continueCapture commandAction = iota
	finishCapture
	abortCapture
)

func (s *Session) publish(show bool) {
	if s.Progress == nil {
		return
	}
	c := s.Controller
	s.Progress(Progress{Preview: c.Stitch.Preview(PreviewWidth, PreviewHeight), Height: c.Stitch.Height(),
		CanUndo: c.Stitch.CanUndo(), Paused: c.Paused, Automatic: c.Automatic,
		Notice: c.Notice, Show: show, SourceEnded: s.sourceEnded})
}

func (s *Session) processCommands() commandAction {
	for {
		select {
		case command, ok := <-s.Commands:
			if !ok {
				return abortCapture
			}
			switch command.Kind {
			case "finish":
				return finishCapture
			case "pause":
				s.Controller.Pause("已暂停，可查看结果或继续截图。")
				s.publish(false)
			case "resume":
				s.Controller.Resume(command.Automatic, time.Now())
				s.publish(false)
			case "undo":
				s.Controller.Undo()
				s.publish(true)
			}
		default:
			return continueCapture
		}
	}
}

// settle stops automatic input immediately while accepting the final settled
// viewport. Finishing a paused or ended source never asks it for more frames.
func (s *Session) settle(ctx context.Context, source Source) bool {
	c := s.Controller
	if c.Paused {
		return ctx.Err() == nil
	}
	c.Automatic = false
	until := time.Now().Add(400 * time.Millisecond)
	for ctx.Err() == nil && time.Now().Before(until) {
		frameCtx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
		frame, err := source.Next(frameCtx)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			break
		}
		if frame != nil {
			viewport, err := Viewport(frame, s.Region, s.SourceSize)
			if err != nil {
				break
			}
			c.Feed(viewport, time.Now())
		}
		changed, _ := c.Tick(time.Now())
		if changed || c.Paused || c.Settled(time.Now()) {
			break
		}
	}
	return ctx.Err() == nil
}

func (s *Session) review(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case command, ok := <-s.Commands:
			if !ok {
				return false
			}
			if command.Kind == "finish" {
				return ctx.Err() == nil
			}
			if command.Kind == "undo" {
				s.Controller.Undo()
				s.publish(false)
			}
		}
	}
}

// Close releases the source after any finished-image handoff has been queued.
func (s *Session) Close() {
	if s.closeSource != nil {
		s.closeSource()
	}
}

// Run takes ownership of source and returns true only for a requested finish.
// A source error pauses capture for review; closing controls or cancelling the
// context ends it without reading a dead stream or spinning on a closed queue.
// Defer Close before calling Run: a successful finish keeps the source alive
// until the caller queues its editor handoff, avoiding portal teardown flashes.
func (s *Session) Run(ctx context.Context, source Source) (finished bool) {
	s.closeSource = sync.OnceFunc(source.Close)
	defer func() {
		if !finished {
			s.Close()
		}
	}()
	c := s.Controller
	s.publish(false)
	for ctx.Err() == nil {
		if action := s.processCommands(); action != continueCapture {
			return action == finishCapture && s.settle(ctx, source)
		}
		frameCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		frame, err := source.Next(frameCtx)
		cancel()
		viewport, frameErr := Viewport(frame, s.Region, s.SourceSize)
		if frameErr != nil {
			err = frameErr
		}
		if action := s.processCommands(); action != continueCapture {
			if action == finishCapture && viewport != nil && frameErr == nil {
				c.Feed(viewport, time.Now())
			}
			return action == finishCapture && s.settle(ctx, source)
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return false
			}
			c.Pause("画面采集已结束，可完成并保存此前结果：" + err.Error())
			s.Close()
			s.sourceEnded = true
			s.publish(true)
			return s.review(ctx)
		}
		if viewport != nil {
			c.Feed(viewport, time.Now())
		}
		changed, scroll := c.Tick(time.Now())
		if scroll {
			err := source.Scroll(ctx, s.Region.Min.Add(s.Region.Size().Div(2)))
			c.Scrolled(time.Now(), err)
			changed = changed || err != nil
		}
		if changed {
			s.publish(c.Paused)
		}
	}
	return false
}

// Viewport accepts cropped frames retaining source coordinates and legacy
// full-source frames, rejecting a changed source or a shifted crop.
func Viewport(frame *image.RGBA, region image.Rectangle, sourceSize image.Point) (*image.RGBA, error) {
	if frame == nil {
		return nil, nil
	}
	if frame.Bounds() == region {
		return frame, nil
	}
	if frame.Bounds() != (image.Rectangle{Max: sourceSize}) || !region.In(frame.Bounds()) {
		return nil, ErrSize
	}
	return frame.SubImage(region).(*image.RGBA), nil
}
