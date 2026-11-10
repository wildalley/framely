package recording

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func box(kind string, payload ...[]byte) []byte {
	size := 8
	for _, p := range payload {
		size += len(p)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(size))
	out = append(out, kind...)
	for _, p := range payload {
		out = append(out, p...)
	}
	return out
}

func mvhd(version byte, scale uint32, units uint64) []byte {
	p := []byte{version, 0, 0, 0}
	if version == 1 {
		p = append(p, make([]byte, 16)...)
		p = binary.BigEndian.AppendUint32(p, scale)
		p = binary.BigEndian.AppendUint64(p, units)
	} else {
		p = append(p, make([]byte, 8)...)
		p = binary.BigEndian.AppendUint32(p, scale)
		p = binary.BigEndian.AppendUint32(p, uint32(units))
	}
	return box("mvhd", append(p, make([]byte, 80)...))
}

func TestDurationReadsMovieHeader(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		data []byte
		want float64
	}{
		"v0-after-mdat": {append(append(box("ftyp", []byte("isom0000")), box("mdat", make([]byte, 4096))...), box("moov", mvhd(0, 1000, 12500))...), 12.5},
		"v1":            {append(box("ftyp", []byte("isom0000")), box("moov", box("trak"), mvhd(1, 90000, 90000*75))...), 75},
	}
	for name, tc := range cases {
		path := filepath.Join(dir, name+".mp4")
		if err := os.WriteFile(path, tc.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := Duration(path); err != nil || got != tc.want {
			t.Fatalf("%s: %v %v", name, got, err)
		}
	}
	for name, data := range map[string][]byte{
		"empty":     nil,
		"truncated": box("moov", mvhd(0, 1000, 1))[:30],
		"zero":      box("moov", mvhd(0, 0, 1)),
		"overrun":   append(binary.BigEndian.AppendUint32(nil, 1<<30), "moov"...),
	} {
		path := filepath.Join(dir, name+".mp4")
		_ = os.WriteFile(path, data, 0o644)
		if _, err := Duration(path); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestAutomaticNamesNeverReplaceFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "新 文件夹")
	now := time.Date(2026, 11, 10, 9, 8, 7, 0, time.Local)
	first, err := NextPath(dir, "录屏", ".mp4", now)
	if err != nil || filepath.Base(first) != "录屏 2026-11-10 09-08-07.mp4" {
		t.Fatal(first, err)
	}
	_ = os.WriteFile(first, nil, 0o644)
	second, _ := NextPath(dir, "录屏", ".mp4", now)
	if filepath.Base(second) != "录屏 2026-11-10 09-08-07 (2).mp4" {
		t.Fatal(second)
	}
	gif := SiblingPath(first, ".gif")
	if gif != filepath.Join(dir, "录屏 2026-11-10 09-08-07.gif") {
		t.Fatal(gif)
	}
	_ = os.WriteFile(gif, nil, 0o644)
	if next := SiblingPath(first, ".gif"); next != filepath.Join(dir, "录屏 2026-11-10 09-08-07 (2).gif") {
		t.Fatal(next)
	}
	if _, err := NextPath("", "录屏", ".mp4", now); err == nil {
		t.Fatal("empty directory accepted")
	}
}
