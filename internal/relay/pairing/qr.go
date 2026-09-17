package pairing

// A self-contained QR code encoder, kept in-tree so rendering a pairing offer
// costs the daemon no third-party dependency. It covers exactly what pairing
// needs: 8-bit byte mode, versions 1 to 15, error-correction levels L and M,
// automatic version selection and standard mask selection.
//
// How this is known to be correct: no QR decoder (zbar, zxing, a phone) is
// reachable from this repo's build environment, so qr_decode_test.go carries
// an independent decoder written against ISO/IEC 18004 — it recovers the mask
// and error-correction level from the format field, unmasks, de-interleaves
// the blocks, checks each one's Reed-Solomon syndromes, and parses the bit
// stream back to bytes. Every symbol this encoder produces reads back through
// it to the exact input string, across both levels, all fifteen versions, all
// 256 byte values, and from the rendered PNG's pixels and the terminal
// rendering as well as the module grid. The published tables it relies on
// (codeword totals, block layouts, format and version strings, capacities) are
// pinned against the standard by the tests in qr_test.go. If a real decoder
// ever becomes available here, point it at Code.PNG and it should agree.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
)

// ErrContentTooLong is returned when content does not fit in the largest
// supported symbol (version 15) at the requested error-correction level.
var ErrContentTooLong = errors.New("pairing: content too long for a QR code")

// ECLevel is a QR error-correction level. Higher levels survive more damage
// but hold fewer bytes; pairing uses ECMedium, which tolerates roughly 15% of
// the symbol being obscured.
type ECLevel int

// The error-correction levels this encoder supports.
const (
	// ECLow recovers about 7% of the symbol.
	ECLow ECLevel = iota
	// ECMedium recovers about 15% of the symbol.
	ECMedium
)

// String implements fmt.Stringer.
func (l ECLevel) String() string {
	switch l {
	case ECLow:
		return "L"
	case ECMedium:
		return "M"
	default:
		return fmt.Sprintf("ECLevel(%d)", int(l))
	}
}

// formatIndicator returns the two-bit level code used in the format
// information field. The levels are not numbered in capacity order there:
// L is 01 and M is 00 (Q is 11 and H is 10).
func (l ECLevel) formatIndicator() int {
	if l == ECLow {
		return 0b01
	}
	return 0b00
}

// valid reports whether l is a level this encoder supports.
func (l ECLevel) valid() bool { return l == ECLow || l == ECMedium }

// Code is a rendered QR symbol: a square grid of dark and light modules plus
// the parameters that produced it.
type Code struct {
	version int
	level   ECLevel
	mask    int
	modules [][]bool
}

// EncodeQR encodes content as a QR symbol at the given error-correction level,
// choosing the smallest version from 1 to 15 that holds it and the data mask
// with the lowest penalty score. It returns an error wrapping
// ErrContentTooLong if content does not fit in a version 15 symbol.
func EncodeQR(content string, level ECLevel) (*Code, error) {
	if !level.valid() {
		return nil, fmt.Errorf("pairing: unsupported error-correction level %s", level)
	}
	version, err := pickVersion(len(content), level)
	if err != nil {
		return nil, err
	}

	m := newMatrix(version, level)
	m.placeData(interleave(encodeData(content, version, level), version, level))

	// Try every mask, keep the one the standard penalty rules like best.
	best, bestScore := 0, math.MaxInt
	for mask := range numMasks {
		m.applyMask(mask)
		m.drawFormat(mask)
		if score := m.penalty(); score < bestScore {
			best, bestScore = mask, score
		}
		m.applyMask(mask) // masking is an XOR, so this undoes it
	}
	m.applyMask(best)
	m.drawFormat(best)

	return &Code{version: version, level: level, mask: best, modules: m.modules}, nil
}

// Version returns the symbol version, 1 to 15.
func (c *Code) Version() int { return c.version }

// Level returns the error-correction level the symbol was encoded at.
func (c *Code) Level() ECLevel { return c.level }

// Mask returns the data mask pattern that was selected, 0 to 7.
func (c *Code) Mask() int { return c.mask }

// Size returns the number of modules per side, excluding the quiet zone.
func (c *Code) Size() int { return len(c.modules) }

// Module reports whether the module at (row, col) is dark. Coordinates outside
// the symbol are light, so callers can read across a quiet zone.
func (c *Code) Module(row, col int) bool {
	if row < 0 || row >= len(c.modules) || col < 0 || col >= len(c.modules) {
		return false
	}
	return c.modules[row][col]
}

// PNG renders the symbol as a greyscale PNG with each module drawn
// moduleSize pixels square and a light border quietZone modules wide. The
// standard quiet zone is 4 modules; anything narrower risks scanners missing
// the symbol's edge.
func (c *Code) PNG(moduleSize, quietZone int) ([]byte, error) {
	if moduleSize < 1 {
		return nil, fmt.Errorf("pairing: module size %d must be at least 1 pixel", moduleSize)
	}
	if quietZone < 0 {
		return nil, fmt.Errorf("pairing: quiet zone %d cannot be negative", quietZone)
	}

	side := (c.Size() + 2*quietZone) * moduleSize
	img := image.NewGray(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for row := range c.Size() {
		for col := range c.Size() {
			if !c.modules[row][col] {
				continue
			}
			x0 := (col + quietZone) * moduleSize
			y0 := (row + quietZone) * moduleSize
			for y := y0; y < y0+moduleSize; y++ {
				for x := x0; x < x0+moduleSize; x++ {
					img.SetGray(x, y, color.Gray{})
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("pairing: encode QR PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// quietZoneModules is the standard quiet zone width, in modules. Scanners
// need this much blank margin around a symbol to find its edges.
const quietZoneModules = 4

// halfBlock returns the glyph for one text cell, given whether the module in
// the upper and lower half of that cell is dark. Light modules get the filled
// parts of the glyph; see Code.Text for why.
func halfBlock(upperDark, lowerDark bool) rune {
	switch {
	case !upperDark && !lowerDark:
		return '█'
	case !upperDark:
		return '▀'
	case !lowerDark:
		return '▄'
	default:
		return ' '
	}
}

// Text renders the symbol for a terminal using Unicode half blocks, two module
// rows per line of text, with the standard quiet zone. It is drawn for a
// dark-background terminal: *light* modules are the filled glyphs, drawn in
// the terminal's foreground colour, and dark modules are left blank. On a
// light background the colours have to be inverted for the code to scan. The
// result ends in a newline and every line is the same width.
func (c *Code) Text() string {
	side := c.Size() + 2*quietZoneModules
	var b strings.Builder
	b.Grow(side * (side/2 + 1) * 4)
	for row := 0; row < side; row += 2 {
		for col := range side {
			upperDark := c.Module(row-quietZoneModules, col-quietZoneModules)
			// An odd number of rows leaves the last line half empty; pad it
			// with quiet zone rather than truncating the symbol.
			lowerDark := row+1 < side && c.Module(row+1-quietZoneModules, col-quietZoneModules)
			b.WriteRune(halfBlock(upperDark, lowerDark))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// QRPNG renders a pairing URL as a PNG QR code at error-correction level M,
// with each module moduleSize pixels square and the standard 4-module quiet
// zone.
func QRPNG(pairURL string, moduleSize int) ([]byte, error) {
	code, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		return nil, err
	}
	return code.PNG(moduleSize, quietZoneModules)
}

// QRText renders a pairing URL as a QR code drawn with terminal half blocks at
// error-correction level M. See Code.Text for the colour convention.
func QRText(pairURL string) (string, error) {
	code, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		return "", err
	}
	return code.Text(), nil
}

// The rest of this file is the offer flow's entry point into the encoder: an
// offer renders itself as a QR code by way of its own deep link, so a caller
// showing a pairing code never has to hold the URL, pick an error-correction
// level, or remember the quiet zone.

// QRCode renders the offer's pairing deep link as a QR symbol at
// error-correction level M. An empty base uses DefaultPairURL. The offer is
// validated first, so an incomplete offer fails here rather than producing a
// QR code that cannot be paired with.
func (o Offer) QRCode(base string) (*Code, error) {
	pairURL, err := o.URL(base)
	if err != nil {
		return nil, err
	}
	return EncodeQR(pairURL, ECMedium)
}

// QRPNG renders the offer's pairing deep link as a PNG QR code, each module
// moduleSize pixels square, with the standard quiet zone. This is what a UI or
// an API response hands to a device that is about to scan.
func (o Offer) QRPNG(base string, moduleSize int) ([]byte, error) {
	code, err := o.QRCode(base)
	if err != nil {
		return nil, err
	}
	return code.PNG(moduleSize, quietZoneModules)
}

// QRText renders the offer's pairing deep link as a QR code drawn with
// terminal half blocks, for showing a pairing code straight in a console. See
// Code.Text for the colour convention.
func (o Offer) QRText(base string) (string, error) {
	code, err := o.QRCode(base)
	if err != nil {
		return "", err
	}
	return code.Text(), nil
}
