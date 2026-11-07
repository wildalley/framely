package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"testing"
)

func TestClosedStreamDropsQueuedFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &processStream{frames: make(chan *image.RGBA, 1), done: make(chan struct{}), cancel: cancel}
	s.frames <- image.NewRGBA(image.Rect(0, 0, 1920, 1080))
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
