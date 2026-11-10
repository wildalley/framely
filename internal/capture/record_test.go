package capture

import (
	"bytes"
	"encoding/binary"
	"io"
	"path/filepath"
	"testing"
)

func TestRecordingStatusProtocol(t *testing.T) {
	for _, tc := range []struct {
		json  string
		valid bool
	}{
		{`{"finished":true,"elapsedMs":2000,"frames":120,"captured":100,"repeated":20,"fps":60,"width":1920,"height":1080,"encoder":"mpeg4"}`, true},
		{`{"fps":60,"frames":1,"repeated":2}`, false},
		{`{"fps":60,"captured":-1}`, false},
		{`{"fps":999}`, false},
		{`{"fps":60,"width":999999999}`, false},
		{`{`, false},
	} {
		header := make([]byte, 16)
		copy(header, "SPR1")
		binary.LittleEndian.PutUint32(header[4:], uint32(len(tc.json)))
		frame, reply, err := readStreamPacket(io.MultiReader(bytes.NewReader(header), bytes.NewBufferString(tc.json)))
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.json, err)
		}
		if tc.valid && (reply != nil || frame.pixels != nil || frame.status == nil || !frame.status.Finished) {
			t.Fatal("status treated as pixels")
		}
	}
}
func TestRecordingOptions(t *testing.T) {
	o := RecordingOptions{Path: filepath.Join(t.TempDir(), "video.mp4"), FPS: 60, MaxEdge: 1920}
	if _, err := o.Arguments(); err != nil {
		t.Fatal(err)
	}
	o.FPS = 120
	if _, err := o.Arguments(); err == nil {
		t.Fatal("unbounded frame rate")
	}
}
