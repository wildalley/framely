package recording

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NextPath returns dir/<prefix> <time><ext>, adding " (2)", " (3)", … when
// a file of that name exists, so automatic saving never replaces a file.
func NextPath(dir, prefix, ext string, now time.Time) (string, error) {
	if dir == "" {
		return "", errors.New("未设置保存目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建保存目录：%w", err)
	}
	base := prefix + " " + now.Format("2006-01-02 15-04-05")
	for n := 1; n < 1000; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s (%d)", base, n)
		}
		path := filepath.Join(dir, name+ext)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
	}
	return "", errors.New("保存目录中同名文件过多")
}

// SiblingPath picks a free name next to source with another extension, as a
// GIF exported from "clip.mp4" becomes "clip.gif" or "clip (2).gif".
func SiblingPath(source, ext string) string {
	base := strings.TrimSuffix(source, filepath.Ext(source))
	for n := 1; n < 1000; n++ {
		path := base + ext
		if n > 1 {
			path = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path
		}
	}
	return base + time.Now().Format(" 150405") + ext
}

// Duration reads the movie header of an MP4/MOV file, in seconds. It walks
// only box headers, so it reads a few hundred bytes even for long videos.
func Duration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	moov, size, err := findBox(f, 0, info.Size(), "moov")
	if err != nil {
		return 0, err
	}
	mvhd, size, err := findBox(f, moov, moov+size, "mvhd")
	if err != nil {
		return 0, err
	}
	var head [32]byte
	n, _ := f.ReadAt(head[:], mvhd)
	var scale, units uint64
	switch {
	case n >= 20 && head[0] == 0:
		scale, units = uint64(binary.BigEndian.Uint32(head[12:])), uint64(binary.BigEndian.Uint32(head[16:]))
	case n >= 32 && head[0] == 1 && size >= 32:
		scale, units = uint64(binary.BigEndian.Uint32(head[20:])), binary.BigEndian.Uint64(head[24:])
	default:
		return 0, errors.New("无法读取视频时长")
	}
	if scale == 0 {
		return 0, errors.New("无法读取视频时长")
	}
	return float64(units) / float64(scale), nil
}

// findBox returns the payload offset and payload size of the first box of
// kind between start and end.
func findBox(r io.ReaderAt, start, end int64, kind string) (int64, int64, error) {
	for off := start; off+8 <= end; {
		var h [16]byte
		if _, err := r.ReadAt(h[:8], off); err != nil {
			return 0, 0, errors.New("视频文件不完整")
		}
		size, header := int64(binary.BigEndian.Uint32(h[:4])), int64(8)
		switch size {
		case 0:
			size = end - off
		case 1:
			if _, err := r.ReadAt(h[8:16], off+8); err != nil {
				return 0, 0, errors.New("视频文件不完整")
			}
			size, header = int64(binary.BigEndian.Uint64(h[8:16])), 16
		}
		if size < header || off+size > end {
			return 0, 0, errors.New("视频文件结构无效")
		}
		if string(h[4:8]) == kind {
			return off + header, size - header, nil
		}
		off += size
	}
	return 0, 0, errors.New("不是可识别的 MP4 视频")
}
