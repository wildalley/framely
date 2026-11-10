package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"testing"
	"time"
)

func TestClosedStreamDropsQueuedFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &processStream{frames: make(chan streamFrame, 1), done: make(chan struct{}), cancel: cancel}
	s.frames <- streamFrame{pixels: image.NewRGBA(image.Rect(0, 0, 1920, 1080))}
	cleanups := 0
	s.cleanup = func() { cleanups++ }
	close(s.done)
	s.Close()
	s.Close()
	if ctx.Err() == nil || cleanups != 1 || len(s.frames) != 0 {
		t.Fatal("closed capture retained a desktop frame or repeated cleanup")
	}
	if _, err := s.Next(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed stream Next() = %v", err)
	}
}

func regionPacket(w, h, x, y, sw, sh, generation uint32, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("SPF2")
	_ = binary.Write(&b, binary.LittleEndian, []uint32{w, h, 2, x, y, sw, sh, generation})
	b.Write(data)
	return b.Bytes()
}

func TestRegionPacketPreservesCoordinatesAndSource(t *testing.T) {
	f, reply, err := readStreamPacket(bytes.NewReader(regionPacket(2, 1, 30, 40, 1920, 1080, 7, []byte{1, 2, 3, 0, 4, 5, 6, 0})))
	if err != nil || reply != nil {
		t.Fatalf("reply=%v error=%v", reply, err)
	}
	if f.source != image.Pt(1920, 1080) || f.generation != 7 || f.protocol != 2 || f.pixels.Rect != image.Rect(30, 40, 32, 41) || f.pixels.Stride != 8 {
		t.Fatalf("wrong crop metadata: %+v", f)
	}
	if got := f.pixels.RGBAAt(31, 40); got.R != 4 || got.G != 5 || got.B != 6 || got.A != 255 {
		t.Fatal(got)
	}
}

func TestRegionPacketRejectsMetadataBeforeReadingPixels(t *testing.T) {
	for _, data := range [][]byte{
		regionPacket(2, 1, 1919, 0, 1920, 1080, 1, nil),
		regionPacket(1, 2, 0, 1079, 1920, 1080, 1, nil),
		regionPacket(1, 1, ^uint32(0), 0, 1920, 1080, 1, nil),
		regionPacket(1, 1, 0, 0, 0, 1080, 1, nil),
		regionPacket(1, 1, 0, 0, 8000, 8000, 1, nil),
	} {
		_, _, err := readStreamPacket(bytes.NewReader(data))
		if err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("metadata was accepted: %v", err)
		}
	}
	valid := regionPacket(1, 1, 0, 0, 1, 1, 1, []byte{1, 2, 3, 255})
	for n := 0; n < len(valid); n++ {
		if _, _, err := readStreamPacket(bytes.NewReader(valid[:n])); err == nil {
			t.Fatalf("accepted truncated packet at %d", n)
		}
	}
}

func TestRegionAcknowledgmentDiscardsQueuedOldFrame(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	s := &processStream{frames: make(chan streamFrame, 1), control: make(chan controlReply, 2), done: make(chan struct{}), input: writer, cancel: func() {}}
	s.protocol.Store(2)
	s.frames <- streamFrame{pixels: image.NewRGBA(image.Rect(0, 0, 20, 20)), generation: 0}
	region := image.Rect(3, 4, 10, 12)
	errCh := make(chan error, 1)
	go func() { errCh <- SetStreamRegion(context.Background(), s, region, image.Pt(20, 20)) }()
	command := make([]byte, len("crop 1 3 4 7 8 20 20\n"))
	if _, err := io.ReadFull(reader, command); err != nil || string(command) != "crop 1 3 4 7 8 20 20\n" {
		t.Fatalf("command %q: %v", command, err)
	}
	s.control <- controlReply{id: 99} // stale acknowledgments cannot complete the request
	s.control <- controlReply{id: 1}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale frame escaped: %v", err)
	}
	want := image.NewRGBA(region)
	s.frames <- streamFrame{pixels: want, generation: 1}
	if got, err := s.Next(context.Background()); err != nil || got != want {
		t.Fatalf("crop lost: %v", err)
	}
}

func TestCancelledCropUnblocksControlWrite(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	s := &processStream{input: w, done: make(chan struct{}), cancel: func() { _ = w.Close() }}
	s.protocol.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := SetStreamRegion(ctx, s, image.Rect(1, 1, 3, 3), image.Pt(10, 10)); err == nil {
		t.Fatal("blocked control write survived cancellation")
	}
}

func BenchmarkStreamFrame(b *testing.B) {
	for _, size := range []image.Point{image.Pt(3840, 2160), image.Pt(800, 1000)} {
		b.Run(size.String(), func(b *testing.B) {
			data := regionPacket(uint32(size.X), uint32(size.Y), 0, 0, 3840, 2160, 1, make([]byte, size.X*size.Y*4))
			b.ReportAllocs()
			b.SetBytes(int64(size.X * size.Y * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := readStreamPacket(bytes.NewReader(data)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func packet(w, h, format uint32, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("SPF1")
	_ = binary.Write(&b, binary.LittleEndian, []uint32{w, h, format})
	b.Write(data)
	return b.Bytes()
}
func TestNativeFramesAreDecodedWithoutRowOrColorSwaps(t *testing.T) {
	img, err := readFrame(bytes.NewReader(packet(2, 1, 1, []byte{10, 20, 30, 0, 40, 50, 60, 255})))
	if err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(0, 0); c.R != 30 || c.G != 20 || c.B != 10 || c.A != 255 {
		t.Fatal(c)
	}
	if c := img.RGBAAt(1, 0); c.R != 60 || c.G != 50 || c.B != 40 {
		t.Fatal(c)
	}
	for _, bad := range [][]byte{packet(0, 1, 1, nil), packet(1, 1, 9, []byte{1, 2, 3, 4}), packet(20000, 1, 1, nil), packet(1, 1, 1, []byte{1, 2, 3})} {
		if _, err := readFrame(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
}
func TestInputFailureDoesNotCorruptTheFollowingFrame(t *testing.T) {
	var data bytes.Buffer
	data.WriteString("SPC1")
	_ = binary.Write(&data, binary.LittleEndian, []uint32{6, 1, 42})
	data.WriteString("denied")
	data.Write(packet(1, 1, 2, []byte{1, 2, 3, 255}))
	img, reply, err := readPacket(&data)
	if err != nil || img != nil || reply == nil || reply.id != 42 || reply.err == nil {
		t.Fatalf("reply=%v err=%v", reply, err)
	}
	img, err = readFrame(&data)
	if err != nil || img.RGBAAt(0, 0).R != 1 {
		t.Fatal("control response damaged following pixels")
	}
}
