package editor

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// Most icons are Lucide's (ISC license, see icons/LICENSE). The capture and
// OCR extensions use the same view box and stroke conventions.
//
//go:embed icons/*.svg
var iconFiles embed.FS

func icon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	iconSelect      = icon("mouse-pointer-2")
	iconRect        = icon("square")
	iconEllipse     = icon("circle")
	iconArrow       = icon("move-up-right")
	iconPen         = icon("pencil")
	iconHighlighter = icon("highlighter")
	iconArea        = icon("square-dashed")
	iconText        = icon("type")
	iconMosaic      = icon("grid-3x3")
	iconPalette     = icon("palette")
	iconUndo        = icon("undo-2")
	iconRedo        = icon("redo-2")
	iconHelp        = icon("circle-question-mark")
	iconSettings    = icon("settings")
	iconPin         = icon("pin")
	iconPinOff      = icon("pin-off")
	iconDownload    = icon("download")
	iconMore        = icon("ellipsis")
	iconCopy        = icon("copy")
	iconClose       = icon("x")
	iconDown        = icon("arrow-down")
	iconUp          = icon("arrow-up")
	iconTrash       = icon("trash-2")
	iconZoomIn      = icon("zoom-in")
	iconZoomOut     = icon("zoom-out")
	iconFit         = icon("maximize-2")
	iconCapture     = icon("scan-line")
	iconScroll      = icon("scroll-capture")
	iconOCR         = icon("text-scan")
	iconClipboard   = icon("clipboard")
	iconImage       = icon("image-plus")
	iconFolder      = icon("folder-open")
	iconArchive     = icon("file-archive")
	iconCrop        = icon("crop")
)
