package ocr

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/wildalley/framely/internal/components"
)

var sharedWorker = workerPool{idleTimeout: 45 * time.Second}

type workerPool struct {
	mu          sync.Mutex
	process     *workerProcess
	timer       *time.Timer
	generation  uint64
	idleTimeout time.Duration
}

type workerProcess struct {
	path    string
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     io.ReadCloser
	log     limitedBuffer
	once    sync.Once
	stopped chan struct{}
}

func (p *workerProcess) stop() {
	p.once.Do(func() {
		_ = p.cmd.Process.Kill()
		_ = p.in.Close()
		_ = p.out.Close()
		_ = p.cmd.Wait()
		close(p.stopped)
	})
}

// Close releases even an idle model process when the application exits.
func Close() { sharedWorker.close() }

func (pool *workerPool) close() {
	pool.mu.Lock()
	pool.generation++
	if pool.timer != nil {
		pool.timer.Stop()
		pool.timer = nil
	}
	p := pool.process
	pool.process = nil
	pool.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (pool *workerPool) acquire(path string) (*workerProcess, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.generation++
	if pool.timer != nil {
		pool.timer.Stop()
		pool.timer = nil
	}
	if p := pool.process; p != nil {
		select {
		case <-p.stopped:
		default:
			if p.path == path {
				return p, nil
			}
		}
		p.stop()
		pool.process = nil
	}
	// Start under the lock so shutdown cannot miss a process being created.
	p := &workerProcess{path: path, cmd: exec.Command(path, "--server"), stopped: make(chan struct{})}
	configureProcess(p.cmd)
	var err error
	p.in, err = p.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p.out, err = p.cmd.StdoutPipe()
	if err != nil {
		p.in.Close()
		return nil, err
	}
	p.log.limit = 16 << 10
	p.cmd.Stderr = &p.log
	if err = p.cmd.Start(); err != nil {
		p.in.Close()
		p.out.Close()
		return nil, err
	}
	pool.process = p
	return p, nil
}

func (pool *workerPool) release(p *workerProcess, healthy bool) {
	pool.mu.Lock()
	if pool.process != p {
		pool.mu.Unlock()
		p.stop()
		return
	}
	if !healthy {
		pool.process = nil
		pool.mu.Unlock()
		p.stop()
		return
	}
	generation := pool.generation
	pool.timer = time.AfterFunc(pool.idleTimeout, func() {
		pool.mu.Lock()
		if pool.process != p || pool.generation != generation {
			pool.mu.Unlock()
			return
		}
		pool.process, pool.timer = nil, nil
		// Finish reaping before acquire can start another model. Otherwise
		// a request arriving at expiry briefly keeps two model processes.
		p.stop()
		pool.mu.Unlock()
	})
	pool.mu.Unlock()
}

func (pool *workerPool) recognize(ctx context.Context, img *image.RGBA, progress func(Progress)) (Result, error) {
	path, err := components.Find("screenpin-ocr")
	if err != nil {
		return Result{}, err
	}
	// Refuse stale installed components promptly instead of interpreting PNG
	// protocol output as a v2 frame length and waiting until the timeout.
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json")); err == nil {
		var manifest struct {
			Protocol int `json:"protocol"`
		}
		if json.Unmarshal(data, &manifest) != nil || manifest.Protocol != 2 {
			return Result{}, errors.New("OCR 组件版本不匹配，请重新安装或构建完整组件")
		}
	}
	p, err := pool.acquire(path)
	if err != nil {
		return Result{}, fmt.Errorf("启动 OCR 失败: %w", err)
	}
	healthy := false
	defer func() { pool.release(p, healthy) }()
	stopCancellation := context.AfterFunc(ctx, p.stop)
	defer stopCancellation()
	if progress != nil {
		progress(Progress{Stage: "recognizing"})
	}
	if err = writePixels(ctx, p.in, img); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("发送 OCR 图片失败: %w", err)
	}
	lineCount, bytesRead, textBytes := 0, 0, 0
	for messages := 0; messages < 1024; messages++ {
		var header [4]byte
		if _, err = io.ReadFull(p.out, header[:]); err != nil {
			break
		}
		n := binary.LittleEndian.Uint32(header[:])
		if n == 0 || n > 16<<20 || bytesRead+int(n) > 32<<20 {
			err = errors.New("OCR 结果超过支持范围")
			break
		}
		bytesRead += int(n)
		data := make([]byte, int(n))
		if _, err = io.ReadFull(p.out, data); err != nil {
			break
		}
		// Explicit fields avoid ambiguous embedded JSON `lines` properties.
		var msg struct {
			Type      string `json:"type"`
			Completed int    `json:"completed"`
			Total     int    `json:"total"`
			Lines     []Line `json:"lines"`
			Model     string `json:"model"`
			Error     string `json:"error"`
		}
		if err = json.Unmarshal(data, &msg); err != nil {
			break
		}
		if msg.Error != "" {
			err = errors.New(msg.Error)
			break
		}
		r := Result{Lines: msg.Lines, Model: msg.Model}
		if err = validate(&r, img.Rect.Dx(), img.Rect.Dy()); err != nil {
			break
		}
		switch msg.Type {
		case "progress":
			lineCount += len(r.Lines)
			for _, line := range r.Lines {
				textBytes += len(line.Text)
			}
			if textBytes > 2<<20 || lineCount > 20000 || msg.Total < 1 || msg.Total > 1024 || msg.Completed < 1 || msg.Completed > msg.Total {
				err = errors.New("OCR 进度无效")
				break
			}
			if progress != nil {
				progress(Progress{Stage: "recognizing", Completed: msg.Completed, Total: msg.Total, Lines: r.Lines})
			}
		case "result":
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			healthy = stopCancellation()
			return r, nil
		default:
			err = errors.New("OCR 协议消息无效")
		}
		if err != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err == nil {
		err = errors.New("OCR 响应过多")
	}
	p.stop() // Wait joins the stderr writer before reading diagnostics.
	return Result{}, fmt.Errorf("OCR 组件失败: %w\n%s", err, p.log.String())
}

func writePixels(ctx context.Context, w io.Writer, img *image.RGBA) error {
	width, height := img.Rect.Dx(), img.Rect.Dy()
	var header [16]byte
	copy(header[:], "SPO2")
	binary.LittleEndian.PutUint32(header[4:8], uint32(width))
	binary.LittleEndian.PutUint32(header[8:12], uint32(height))
	binary.LittleEndian.PutUint32(header[12:], uint32(width*height*3))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	row := make([]byte, width*3)
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		pixels := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < width; x++ {
			// Go RGBA is premultiplied. Composite on white before transport;
			// hidden RGB under transparent corners must never enter OCR.
			a := 255 - int(pixels[x*4+3])
			for c := 0; c < 3; c++ {
				row[x*3+c] = byte(min(255, int(pixels[x*4+c])+a))
			}
		}
		if _, err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}
