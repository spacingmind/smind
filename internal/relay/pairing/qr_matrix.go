package pairing

// Symbol geometry: function patterns, data placement, masking and the mask
// penalty score, per ISO/IEC 18004.

// numMasks is the number of data mask patterns.
const numMasks = 8

// Penalty weights for mask selection.
const (
	penaltyN1 = 3
	penaltyN2 = 3
	penaltyN3 = 40
	penaltyN4 = 10
)

// BCH generator polynomials for the format and version information fields.
// formatXOR is the mask applied to format information so that it is never all
// zero, which would be indistinguishable from a blank symbol.
const (
	bchFormat  = 0x537
	bchVersion = 0x1F25
	formatXOR  = 0x5412
)

// alignmentCentres[v-1] lists the row/column coordinates of alignment pattern
// centres for version v. Patterns sit at every pair of coordinates except the
// three that would collide with a finder pattern.
var alignmentCentres = [maxVersion][]int{
	nil,
	{6, 18},
	{6, 22},
	{6, 26},
	{6, 30},
	{6, 34},
	{6, 22, 38},
	{6, 24, 42},
	{6, 26, 46},
	{6, 28, 50},
	{6, 30, 54},
	{6, 32, 58},
	{6, 34, 62},
	{6, 26, 46, 66},
	{6, 26, 48, 70},
}

// symbolSize returns the number of modules per side of a version-v symbol.
func symbolSize(version int) int { return 17 + 4*version }

// matrix is a symbol under construction: the module colours plus a parallel
// grid marking which modules are function patterns and therefore neither
// carry data nor get masked.
type matrix struct {
	version int
	level   ECLevel
	size    int
	modules [][]bool
	fn      [][]bool
}

// newMatrix returns a matrix for the given version and level with every
// function pattern drawn and every function module reserved.
func newMatrix(version int, level ECLevel) *matrix {
	size := symbolSize(version)
	m := &matrix{version: version, level: level, size: size}
	m.modules = make([][]bool, size)
	m.fn = make([][]bool, size)
	for i := range size {
		m.modules[i] = make([]bool, size)
		m.fn[i] = make([]bool, size)
	}
	m.drawFunctionPatterns()
	return m
}

// setFunction paints a function module and marks it reserved. Coordinates
// outside the symbol are ignored so pattern drawing can run off the edge.
func (m *matrix) setFunction(row, col int, dark bool) {
	if row < 0 || row >= m.size || col < 0 || col >= m.size {
		return
	}
	m.modules[row][col] = dark
	m.fn[row][col] = true
}

func (m *matrix) drawFunctionPatterns() {
	m.drawFinders()
	m.drawTiming()
	m.drawAlignment()
	// Reserve the format information areas; the real bits are rewritten once a
	// mask has been chosen. The dark module never changes.
	m.drawFormat(0)
	m.setFunction(4*m.version+9, 8, true)
	m.drawVersion()
}

// drawFinders draws the three finder patterns and the light separators that
// surround them.
func (m *matrix) drawFinders() {
	corners := [3][2]int{{0, 0}, {0, m.size - 7}, {m.size - 7, 0}}
	for _, corner := range corners {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				m.setFunction(corner[0]+dr, corner[1]+dc, finderDark(dr, dc))
			}
		}
	}
}

// finderDark reports the colour of the module at (dr, dc) relative to a finder
// pattern's top-left corner. Anything outside the 7x7 pattern is separator and
// therefore light.
func finderDark(dr, dc int) bool {
	if dr < 0 || dr > 6 || dc < 0 || dc > 6 {
		return false
	}
	onRing := dr == 0 || dr == 6 || dc == 0 || dc == 6
	inCore := dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4
	return onRing || inCore
}

// drawTiming draws the alternating timing patterns on row 6 and column 6,
// between the finder separators.
func (m *matrix) drawTiming() {
	for i := 8; i < m.size-8; i++ {
		dark := i%2 == 0
		m.setFunction(6, i, dark)
		m.setFunction(i, 6, dark)
	}
}

// drawAlignment draws the 5x5 alignment patterns, skipping the three
// coordinate pairs that would overlap a finder pattern.
func (m *matrix) drawAlignment() {
	centres := alignmentCentres[m.version-1]
	if len(centres) == 0 {
		return
	}
	last := centres[len(centres)-1]
	for _, row := range centres {
		for _, col := range centres {
			corner := (row == 6 && col == 6) ||
				(row == 6 && col == last) ||
				(row == last && col == 6)
			if corner {
				continue
			}
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					dark := absInt(dr) == 2 || absInt(dc) == 2 || (dr == 0 && dc == 0)
					m.setFunction(row+dr, col+dc, dark)
				}
			}
		}
	}
}

// formatBits returns the 15-bit format information field for a level and mask:
// five data bits protected by a BCH(15, 5) code and XORed with a fixed mask.
func formatBits(level ECLevel, mask int) int {
	data := level.formatIndicator()<<3 | mask
	rem := data
	for range 10 {
		rem = rem<<1 ^ (rem>>9)*bchFormat
	}
	return (data<<10 | rem&0x3FF) ^ formatXOR
}

// drawFormat writes both copies of the format information for the given mask.
func (m *matrix) drawFormat(mask int) {
	bits := formatBits(m.level, mask)
	get := func(i int) bool { return bits>>i&1 == 1 }

	// Copy one, wrapped around the top-left finder: bits 0-5 down column 8,
	// then the corner, then bits 9-14 back along row 8.
	for i := range 6 {
		m.setFunction(i, 8, get(i))
	}
	m.setFunction(7, 8, get(6))
	m.setFunction(8, 8, get(7))
	m.setFunction(8, 7, get(8))
	for i := 9; i < 15; i++ {
		m.setFunction(8, 14-i, get(i))
	}

	// Copy two: bits 0-7 along row 8 beside the top-right finder, bits 8-14
	// down column 8 beside the bottom-left one.
	for i := range 8 {
		m.setFunction(8, m.size-1-i, get(i))
	}
	for i := 8; i < 15; i++ {
		m.setFunction(m.size-15+i, 8, get(i))
	}
}

// versionBits returns the 18-bit version information field: six version bits
// protected by a BCH(18, 6) code.
func versionBits(version int) int {
	rem := version
	for range 12 {
		rem = rem<<1 ^ (rem>>11)*bchVersion
	}
	return version<<12 | rem&0xFFF
}

// drawVersion writes the two copies of the version information field, which
// only exists from version 7 onwards.
func (m *matrix) drawVersion() {
	if m.version < 7 {
		return
	}
	bits := versionBits(m.version)
	for i := range 18 {
		dark := bits>>i&1 == 1
		row, col := i/3, m.size-11+i%3
		m.setFunction(row, col, dark)
		m.setFunction(col, row, dark)
	}
}

// placeData writes the interleaved codewords into the symbol's free modules:
// two-module-wide columns walked from the right edge leftwards, skipping
// column 6, zigzagging upwards then downwards, right module of each pair
// first. Modules left over once the codewords run out — the remainder bits —
// stay light.
func (m *matrix) placeData(codewords []byte) {
	bit, totalBits := 0, len(codewords)*8
	upward := true
	for right := m.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			// Column 6 is the vertical timing pattern, so this pair of columns
			// is 4 and 5 rather than 5 and 6.
			right = 5
		}
		for i := range m.size {
			row := i
			if upward {
				row = m.size - 1 - i
			}
			for j := range 2 {
				col := right - j
				if m.fn[row][col] {
					continue
				}
				dark := false
				if bit < totalBits {
					dark = codewords[bit/8]>>(7-bit%8)&1 == 1
					bit++
				}
				m.modules[row][col] = dark
			}
		}
		upward = !upward
	}
}

// freeModules counts the modules available for data, i.e. everything that is
// not a function pattern.
func (m *matrix) freeModules() int {
	n := 0
	for _, row := range m.fn {
		for _, isFn := range row {
			if !isFn {
				n++
			}
		}
	}
	return n
}

// maskDark reports whether mask pattern n inverts the module at (row, col).
func maskDark(n, row, col int) bool {
	switch n {
	case 0:
		return (row+col)%2 == 0
	case 1:
		return row%2 == 0
	case 2:
		return col%3 == 0
	case 3:
		return (row+col)%3 == 0
	case 4:
		return (row/2+col/3)%2 == 0
	case 5:
		return row*col%2+row*col%3 == 0
	case 6:
		return (row*col%2+row*col%3)%2 == 0
	case 7:
		return ((row+col)%2+row*col%3)%2 == 0
	default:
		return false
	}
}

// applyMask XORs a mask pattern over every data module. Applying the same mask
// a second time restores the unmasked modules.
func (m *matrix) applyMask(mask int) {
	for row := range m.size {
		for col := range m.size {
			if !m.fn[row][col] && maskDark(mask, row, col) {
				m.modules[row][col] = !m.modules[row][col]
			}
		}
	}
}

// penalty scores a masked symbol by the four standard rules; the lowest
// scoring mask is the one that gets used.
func (m *matrix) penalty() int {
	return m.penaltyLines() + m.penaltyBlocks() + m.penaltyDark()
}

// finderLike patterns are the 1:1:3:1:1 finder run with four light modules on
// one side; wherever one turns up in the data it can be mistaken for a finder
// pattern, so rule 3 penalises it.
var finderLike = [2][11]bool{
	{true, false, true, true, true, false, true, false, false, false, false},
	{false, false, false, false, true, false, true, true, true, false, true},
}

// penaltyLines applies rules 1 and 3 to every row and column.
func (m *matrix) penaltyLines() int {
	score := 0
	line := make([]bool, m.size)
	for i := range m.size {
		for j := range m.size {
			line[j] = m.modules[i][j]
		}
		score += linePenalty(line)
		for j := range m.size {
			line[j] = m.modules[j][i]
		}
		score += linePenalty(line)
	}
	return score
}

// linePenalty scores one row or column for rule 1 (runs of five or more
// same-coloured modules) and rule 3 (finder-like patterns).
func linePenalty(line []bool) int {
	score := 0
	run := 1
	for i := 1; i < len(line); i++ {
		if line[i] == line[i-1] {
			run++
			continue
		}
		if run >= 5 {
			score += penaltyN1 + run - 5
		}
		run = 1
	}
	if run >= 5 {
		score += penaltyN1 + run - 5
	}

	for i := 0; i+len(finderLike[0]) <= len(line); i++ {
		for _, want := range finderLike {
			if matchWindow(line[i:], want) {
				score += penaltyN3
			}
		}
	}
	return score
}

// matchWindow reports whether line starts with the given pattern.
func matchWindow(line []bool, want [11]bool) bool {
	for i, w := range want {
		if line[i] != w {
			return false
		}
	}
	return true
}

// penaltyBlocks applies rule 2: every 2x2 block of a single colour.
func (m *matrix) penaltyBlocks() int {
	score := 0
	for row := 0; row+1 < m.size; row++ {
		for col := 0; col+1 < m.size; col++ {
			c := m.modules[row][col]
			if m.modules[row][col+1] == c && m.modules[row+1][col] == c && m.modules[row+1][col+1] == c {
				score += penaltyN2
			}
		}
	}
	return score
}

// penaltyDark applies rule 4: how far the proportion of dark modules strays
// from half, in whole steps of five percent.
func (m *matrix) penaltyDark() int {
	dark := 0
	for _, row := range m.modules {
		for _, isDark := range row {
			if isDark {
				dark++
			}
		}
	}
	total := m.size * m.size
	return absInt(dark*100/total-50) / 5 * penaltyN4
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
