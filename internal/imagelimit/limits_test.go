package imagelimit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigRejectsHugePNGWithoutDecoding(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 10000)
	binary.BigEndian.PutUint32(data[20:24], 10000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, _, err := Config(data); !errors.Is(err, ErrDimensions) {
		t.Fatalf("oversized header accepted: %v", err)
	}
	if err := Dimensions(1000, 32000); err != nil {
		t.Fatal(err)
	}
	if err := Dimensions(32769, 1); err == nil {
		t.Fatal("oversized side accepted")
	}
}

func TestReadRejectsOversizedFileBeforeAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxEncodedBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := ReadFile(path); !errors.Is(err, ErrEncodedSize) {
		t.Fatalf("oversized file accepted: %v", err)
	}
	if _, err := ReadFile(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestBudgetConcurrentOwnersAndFailedAdmission(t *testing.T) {
	b := NewBudget(100)
	l, err := b.Acquire(80)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Acquire(21); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 50; i++ {
		hold := l.Retain()
		workers.Add(1)
		go func() { defer workers.Done(); hold.Release(); hold.Release() }()
	}
	workers.Wait()
	if b.Used() != 80 {
		t.Fatal("tasks released the window's reservation")
	}
	hold := l.Retain()
	l.Release()
	if b.Used() != 80 {
		t.Fatal("closing a window undercounted its unfinished task")
	}
	if l.Retain() != nil {
		t.Fatal("released owner was revived")
	}
	hold.Release()
	if b.Used() != 0 {
		t.Fatal("completed task leaked budget")
	}
	if _, err := b.Acquire(101); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
}
