package doc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // decode JPEG captures and projects
	_ "image/png"
	"math"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // decode WebP projects written by the web editor
)

// Image is the picture a document annotates, kept in its original encoding.
type Image struct {
	Data   []byte // PNG, JPEG or WebP
	Mime   string // image/png, image/jpeg or image/webp
	Width  int
	Height int
	Name   string
}

// Limits a project must keep, as the web editor checked them.
const (
	maxSide        = 32768
	maxPixels      = 100_000_000
	maxAnnotations = 10_000
	maxPoints      = 500_000
	maxDataURL     = 150_000_000
)

// ErrNotImage is returned for data that is not a supported image.
var ErrNotImage = errors.New("不支持的图片格式")

// DecodeImage reads the size and format of encoded image data without
// decoding its pixels.
func DecodeImage(data []byte, name string) (Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, ErrNotImage
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if mime == "" {
		return Image{}, ErrNotImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return Image{}, errors.New("图片尺寸超出支持范围")
	}
	return Image{Data: data, Mime: mime, Width: cfg.Width, Height: cfg.Height, Name: name}, nil
}

// DataURL returns the image as a data: URL, as projects store it.
func (im Image) DataURL() string {
	return "data:" + im.Mime + ";base64," + base64.StdEncoding.EncodeToString(im.Data)
}

// Project is the .screenpin file: the original image, the annotations and
// their order. It is JSON, the format the web editor wrote.
type Project struct {
	Format      string       `json:"format"`
	Version     int          `json:"version"`
	Title       string       `json:"title"`
	CreatedAt   string       `json:"createdAt"`
	UpdatedAt   string       `json:"updatedAt"`
	Image       projectImage `json:"image"`
	Annotations []Annotation `json:"annotations"`
}

type projectImage struct {
	DataURL string  `json:"dataUrl"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Name    string  `json:"name"`
}

// Timestamp formats t as projects store times (JavaScript's toISOString).
func Timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// EncodeProject writes a project file.
func EncodeProject(title, createdAt string, im Image, list []Annotation, now time.Time) ([]byte, error) {
	if list == nil {
		list = []Annotation{}
	}
	return json.Marshal(Project{
		Format:    "screenpin-project",
		Version:   1,
		Title:     title,
		CreatedAt: createdAt,
		UpdatedAt: Timestamp(now),
		Image: projectImage{
			DataURL: im.DataURL(),
			Width:   float64(im.Width),
			Height:  float64(im.Height),
			Name:    im.Name,
		},
		Annotations: list,
	})
}

var dataURLPrefix = regexp.MustCompile(`(?i)^data:image/(png|jpeg|webp);base64,`)

// DecodeProject reads and checks a project file: its fields, its limits,
// and that its image decodes to the size it claims.
func DecodeProject(data []byte) (*Project, Image, error) {
	invalid := errors.New("不是有效的 ScreenPin 工程")
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, Image{}, invalid
	}
	if p.Format != "screenpin-project" || p.Version != 1 || len([]rune(p.Title)) > 500 ||
		!isDate(p.CreatedAt) || !isDate(p.UpdatedAt) || p.Annotations == nil {
		return nil, Image{}, invalid
	}
	pi := p.Image
	if len(pi.DataURL) > maxDataURL || !dataURLPrefix.MatchString(pi.DataURL) ||
		!wholeSize(pi.Width) || !wholeSize(pi.Height) || pi.Width*pi.Height > maxPixels ||
		len([]rune(pi.Name)) > 500 {
		return nil, Image{}, invalid
	}
	if len(p.Annotations) > maxAnnotations {
		return nil, Image{}, invalid
	}
	ids := map[string]bool{}
	points := 0
	for i := range p.Annotations {
		a := &p.Annotations[i]
		if !validAnnotation(a) || ids[a.ID] {
			return nil, Image{}, invalid
		}
		ids[a.ID] = true
		points += len(a.Points)
		if points > maxPoints {
			return nil, Image{}, invalid
		}
	}
	raw, err := base64.StdEncoding.DecodeString(pi.DataURL[strings.IndexByte(pi.DataURL, ',')+1:])
	if err != nil {
		return nil, Image{}, errors.New("工程中的原图已损坏或格式不受支持")
	}
	im, err := DecodeImage(raw, pi.Name)
	if err != nil {
		return nil, Image{}, errors.New("工程中的原图已损坏或格式不受支持")
	}
	if float64(im.Width) != pi.Width || float64(im.Height) != pi.Height {
		return nil, Image{}, errors.New("工程中的原图尺寸与工程数据不一致")
	}
	if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
		return nil, Image{}, errors.New("工程中的原图已损坏或格式不受支持")
	}
	return &p, im, nil
}

func isDate(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z07:00", "2006-01-02"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

func wholeSize(v float64) bool { return v > 0 && v <= maxSide && v == math.Trunc(v) }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func coordinate(v float64) bool { return finite(v) && math.Abs(v) <= 1_000_000 }

var hexColor = regexp.MustCompile(`(?i)^#[0-9a-f]{6}$`)

func validAnnotation(a *Annotation) bool {
	known := a.Type != ToolSelect
	if known {
		known = false
		for _, t := range DrawTools {
			known = known || t == a.Type
		}
	}
	if a.ID == "" || len(a.ID) > 200 || !known ||
		!coordinate(a.X) || !coordinate(a.Y) || !coordinate(a.Width) || !coordinate(a.Height) ||
		!hexColor.MatchString(a.Color) || !(a.Fill == "transparent" || hexColor.MatchString(a.Fill)) ||
		!finite(a.StrokeWidth) || a.StrokeWidth < 0 || a.StrokeWidth > 512 ||
		!finite(a.Opacity) || a.Opacity < 0 || a.Opacity > 1 ||
		!finite(a.FontSize) || a.FontSize < 1 || a.FontSize > 1000 {
		return false
	}
	if a.EndX != nil && !coordinate(*a.EndX) || a.EndY != nil && !coordinate(*a.EndY) {
		return false
	}
	if a.Type == ToolArrow && (a.EndX == nil || a.EndY == nil) {
		return false
	}
	if a.Type == ToolText && (a.Text == nil || len([]rune(*a.Text)) > 100_000) {
		return false
	}
	if a.Type != ToolText && a.Text != nil {
		return false
	}
	if a.Type == ToolMosaic && a.Opacity != 1 {
		return false
	}
	if a.Type.Freehand() {
		if len(a.Points) == 0 || len(a.Points) > 50_000 {
			return false
		}
		for _, p := range a.Points {
			if !coordinate(p.X) || !coordinate(p.Y) {
				return false
			}
		}
	} else if a.Points != nil {
		return false
	}
	return true
}

// Fingerprint identifies what a save would write, apart from the time of
// the save, to tell whether a document has unsaved changes.
func Fingerprint(title, createdAt string, im *Image, list []Annotation) string {
	if im == nil {
		return ""
	}
	b, _ := json.Marshal(list)
	return fmt.Sprintf("%p:%d|%s|%s|%s", im.Data, len(im.Data), title, createdAt, b)
}
