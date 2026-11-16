package scrollshot

import (
	"context"
	"errors"
	"image"
	"io"
	"strings"
	"testing"
	"time"
)

type sessionStream struct {
	frame    *image.RGBA
	err      error
	scrolled bool
	reads    int
	closes   int
}

func (s *sessionStream) Next(ctx context.Context) (*image.RGBA, error) {
	s.reads++
	if s.frame != nil {
		frame := s.frame
		s.frame = nil
		return frame, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *sessionStream) Scroll(context.Context, image.Point) error { s.scrolled = true; return nil }
func (s *sessionStream) Close()                                    { s.closes++ }

func TestFinishKeepsLastSettledScrollAndStopsAutomaticInput(t *testing.T) {
	for _, origin := range []image.Point{{}, {X: 930, Y: 500}} {
		t.Run(origin.String(), func(t *testing.T) {
			page := image.NewRGBA(image.Rect(0, 0, 160, 400))
			seed := uint32(1234)
			for i := 0; i < len(page.Pix); i += 4 {
				seed = seed*1664525 + 1013904223
				page.Pix[i], page.Pix[i+1], page.Pix[i+2], page.Pix[i+3] = uint8(seed>>24), uint8(seed>>16), uint8(seed>>8), 255
			}
			region := image.Rect(0, 0, 160, 240)
			controller, err := NewController(page.SubImage(region).(*image.RGBA), true, time.Now().Add(-time.Second))
			if err != nil {
				t.Fatal(err)
			}
			defer controller.Stitch.Release()
			frame := image.NewRGBA(region)
			for y := 0; y < 240; y++ {
				copy(frame.Pix[y*frame.Stride:(y+1)*frame.Stride], page.Pix[(y+45)*page.Stride:(y+46)*page.Stride])
			}
			frame.Rect = frame.Rect.Add(origin)
			region = region.Add(origin)
			source := &sessionStream{frame: frame}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			commands := make(chan Command, 1)
			commands <- Command{Kind: "finish"}
			session := Session{Controller: controller, Region: region, SourceSize: image.Pt(1920, 1080), Commands: commands}
			defer session.Close()
			finished := session.Run(ctx, source)
			if source.closes != 0 {
				t.Fatal("successful finish tore down the source before the editor handoff")
			}
			session.Close()
			if !finished || controller.Stitch.Height() != 285 || source.scrolled || source.closes != 1 {
				t.Fatalf("height=%d scrolled=%v closes=%d", controller.Stitch.Height(), source.scrolled, source.closes)
			}
		})
	}
}

func TestViewportRejectsChangedSourceAndWrongCrop(t *testing.T) {
	region, size := image.Rect(30, 40, 130, 240), image.Pt(1920, 1080)
	for _, bounds := range []image.Rectangle{region, {Max: size}} {
		got, err := Viewport(image.NewRGBA(bounds), region, size)
		if err != nil || got.Rect != region {
			t.Fatalf("valid frame: %v, %v", got, err)
		}
	}
	for _, bounds := range []image.Rectangle{region.Add(image.Pt(1, 0)), image.Rect(0, 0, 100, 200), image.Rect(0, 0, 1920, 1200)} {
		if _, err := Viewport(image.NewRGBA(bounds), region, size); !errors.Is(err, ErrSize) {
			t.Fatalf("accepted wrong source: %v", err)
		}
	}
}

func TestSessionReviewsDeadSourceWithoutReadingOrResumingIt(t *testing.T) {
	controller, err := NewController(image.NewRGBA(image.Rect(0, 0, 160, 240)), true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Stitch.Release()
	source := &sessionStream{err: io.EOF}
	commands := make(chan Command, 3)
	var progress []Progress
	session := Session{Controller: controller, Region: image.Rect(0, 0, 160, 240), SourceSize: image.Pt(160, 240), Commands: commands,
		Progress: func(next Progress) {
			progress = append(progress, next)
			if next.SourceEnded && next.Show {
				commands <- Command{Kind: "resume", Automatic: true}
				commands <- Command{Kind: "undo"}
				commands <- Command{Kind: "finish"}
			}
		}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !session.Run(ctx, source) || source.reads != 1 || source.closes != 1 || source.scrolled || !controller.Paused {
		t.Fatalf("dead-source review: reads=%d closes=%d paused=%v", source.reads, source.closes, controller.Paused)
	}
	if len(progress) != 3 || !progress[1].SourceEnded || !progress[1].Show || !strings.Contains(progress[1].Notice, "EOF") {
		t.Fatalf("progress=%+v", progress)
	}
	// Subsequent controller mutations cannot change a queued preview.
	before := progress[0].Preview.Pix[0]
	controller.Stitch.previous.Pix[0] = before + 1
	if progress[0].Preview.Pix[0] != before {
		t.Fatal("queued progress shared mutable controller pixels")
	}
	controller.Stitch.Release()
	if progress[0].Preview.Pix[0] != before || progress[0].Height != 240 {
		t.Fatal("queued progress retained live controller state")
	}
}

func TestSessionCancellationAndClosedControlsReleaseSource(t *testing.T) {
	for _, mode := range []string{"closed-controls", "cancel", "cancel-review"} {
		t.Run(mode, func(t *testing.T) {
			controller, err := NewController(image.NewRGBA(image.Rect(0, 0, 160, 240)), false, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer controller.Stitch.Release()
			source := &sessionStream{}
			commands := make(chan Command)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "closed-controls" {
				close(commands)
			}
			if mode == "cancel-review" {
				source.err = io.EOF
			}
			session := Session{Controller: controller, Region: image.Rect(0, 0, 160, 240), SourceSize: image.Pt(160, 240), Commands: commands,
				Progress: func(next Progress) {
					if mode == "cancel" || mode == "cancel-review" && next.SourceEnded {
						cancel()
					}
				}}
			if session.Run(ctx, source) || source.closes != 1 || source.reads > 1 {
				t.Fatalf("abort kept capturing: reads=%d closes=%d", source.reads, source.closes)
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatal("closed controls waited for a timeout")
			}
		})
	}
}
