package qaframes

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// MaxFramesPerSheet is the cap on images in one contact sheet (ADR-0045 §7):
// a model reads at most this many at once.
const MaxFramesPerSheet = 12

// Cell is one frame on a sheet: the image file and its label, which always
// carries the timestamp in milliseconds since its step began.
type Cell struct {
	File  string `json:"file"`
	Label string `json:"label"`
	TMs   int    `json:"tMs"`
}

// SheetInfo describes one written contact sheet.
type SheetInfo struct {
	File  string `json:"file"`
	Cells []Cell `json:"cells"`
}

// ChunkCells splits cells into runs of at most MaxFramesPerSheet.
func ChunkCells(cells []Cell) [][]Cell {
	var out [][]Cell
	for len(cells) > 0 {
		n := min(len(cells), MaxFramesPerSheet)
		out = append(out, cells[:n])
		cells = cells[n:]
	}
	return out
}

const (
	sheetCellW = 320
	sheetLabel = 18
	sheetPad   = 6
)

// WriteSheets composes cells into PNG contact sheets named <stem>-sheetN.png
// under dir, at most MaxFramesPerSheet cells each, and returns them. Cells
// whose image cannot be read are drawn as a grey placeholder, never dropped.
func WriteSheets(dir, stem string, cells []Cell) ([]SheetInfo, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var out []SheetInfo
	for i, chunk := range ChunkCells(cells) {
		img := composeSheet(chunk)
		name := fmt.Sprintf("%s-sheet%d.png", stem, i+1)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		err = png.Encode(f, img)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		out = append(out, SheetInfo{File: name, Cells: chunk})
	}
	return out, nil
}

func composeSheet(cells []Cell) *image.RGBA {
	cols := 4
	if len(cells) < cols {
		cols = max(len(cells), 1)
	}
	rows := (len(cells) + cols - 1) / cols
	// cell height follows the first readable image's aspect ratio
	cellH := sheetCellW * 5 / 8
	imgs := make([]image.Image, len(cells))
	for i, c := range cells {
		if f, err := os.Open(c.File); err == nil {
			if im, err := png.Decode(f); err == nil {
				imgs[i] = im
				if b := im.Bounds(); b.Dx() > 0 && i == 0 {
					cellH = sheetCellW * b.Dy() / b.Dx()
				}
			}
			f.Close()
		}
	}
	w := cols*(sheetCellW+sheetPad) + sheetPad
	h := rows*(cellH+sheetLabel+sheetPad) + sheetPad
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{0x80, 0x80, 0x80, 0xff}), image.Point{}, draw.Src)
	for i, c := range cells {
		x := sheetPad + (i%cols)*(sheetCellW+sheetPad)
		y := sheetPad + (i/cols)*(cellH+sheetLabel+sheetPad)
		drawText(out, x+2, y+2, c.Label, color.RGBA{0xff, 0xff, 0xff, 0xff})
		r := image.Rect(x, y+sheetLabel, x+sheetCellW, y+sheetLabel+cellH)
		if imgs[i] == nil {
			draw.Draw(out, r, image.NewUniform(color.RGBA{0x40, 0x40, 0x40, 0xff}), image.Point{}, draw.Src)
			continue
		}
		scaleInto(out, r, imgs[i])
	}
	return out
}

// scaleInto draws src scaled (nearest neighbour) to fill r.
func scaleInto(dst *image.RGBA, r image.Rectangle, src image.Image) {
	sb := src.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sy := sb.Min.Y + (y-r.Min.Y)*sb.Dy()/r.Dy()
		for x := r.Min.X; x < r.Max.X; x++ {
			sx := sb.Min.X + (x-r.Min.X)*sb.Dx()/r.Dx()
			dst.Set(x, y, src.At(sx, sy))
		}
	}
}

// drawText draws s at (x, y) at 2x scale in the 3x5 font.
func drawText(dst *image.RGBA, x, y int, s string, c color.Color) {
	const k = 2
	for _, r := range s {
		g := glyph(r)
		for gy, row := range g {
			for gx, ch := range row {
				if ch != '#' {
					continue
				}
				for dy := 0; dy < k; dy++ {
					for dx := 0; dx < k; dx++ {
						dst.Set(x+gx*k+dx, y+gy*k+dy, c)
					}
				}
			}
		}
		x += 4 * k
	}
}
