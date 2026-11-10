package capture

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in probe uses the same OS capture authorization as the application.
// It never clicks or grants a sharing permission on the user's behalf.
func TestLiveRecording(t *testing.T) {
	if os.Getenv("SCREENPIN_LIVE_RECORDING") != "1" {
		t.Skip("opt-in live desktop recording")
	}
	directory := os.Getenv("SCREENPIN_RECORDING_OUTPUT")
	if directory == "" || !filepath.IsAbs(directory) {
		t.Fatal("SCREENPIN_RECORDING_OUTPUT must be an absolute directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(directory, "live-60fps-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source, err := OpenStream(ctx, StreamOptions{Recording: &RecordingOptions{Path: path, FPS: 60, MaxEdge: 1920}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	first, err := source.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	size := first.Bounds().Size()
	t.Logf("Capture source: %dx%d", size.X, size.Y)
	first = nil
	if err := SetStreamRegion(ctx, source, image.Rectangle{Max: size}, size); err != nil {
		t.Fatal(err)
	}
	if err := RecordingCommand(ctx, source, "record"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if err := RecordingCommand(ctx, source, "pause"); err != nil {
		t.Fatal(err)
	}
	paused, err := RecordingSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	still, err := RecordingSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	if !still.Paused || still.ElapsedMS != paused.ElapsedMS || still.Frames != paused.Frames {
		t.Fatalf("pause advanced: %+v -> %+v", paused, still)
	}
	if err := RecordingCommand(ctx, source, "resume"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if err := RecordingCommand(ctx, source, "finish"); err != nil {
		source.Close()
		t.Fatal(StreamDiagnostic(source, err))
	}
	status, err := RecordingSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Finished || status.Frames < 60 || status.ElapsedMS < 5500 || status.ElapsedMS > 7000 {
		t.Fatalf("unexpected final status: %+v", status)
	}
	source.Close()
	t.Logf("Saved %s: %+v", path, status)
	if stream, ok := source.(*processStream); ok {
		t.Log(stream.log.String())
	}
}
