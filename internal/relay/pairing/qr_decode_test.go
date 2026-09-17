package pairing

// A QR *decoder*, written only for these tests, that reads a finished symbol
// back the way a scanner does: recover the format information, take the mask
// and error-correction level from it, unmask, walk the data modules,
// de-interleave the blocks, check each block's Reed-Solomon syndromes, and
// parse the bit stream back into the original string.
//
// It exists because no QR decoder (zbar, zxing, a phone) is reachable from
// this repo's build environment, so there is nothing to check the encoder
// against end to end. This stands in for one. It deliberately re-derives
// everything it can rather than calling the encoder: the reserved-module
// layout is rebuilt from the specification's rectangles instead of reusing
// matrix.fn (TestReservedLayoutMatchesEncoder then pins the two against each
// other), the BCH fields are verified by polynomial division rather than by
// regenerating them, and the Reed-Solomon check evaluates each codeword
// polynomial at the generator's roots instead of redoing the long division in
// rsEncode. What it shares with the encoder is the published tables — block
// layouts, codeword totals, alignment centres — which the other tests already
// pin against the values in ISO/IEC 18004.
//
// A symbol that survives this round trip carries the exact input bytes, with
// valid error correction, under a format field a scanner can read. If a real
// decoder is ever available here, it should agree with this one.

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

// decodedSymbol is everything a scanner recovers from a symbol.
type decodedSymbol struct {
	version int
	level   ECLevel
	mask    int
	content string
}

// bitLen returns the number of significant bits in v.
func bitLen(v int) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// polyRemainder divides v by gen over GF(2) and returns the remainder. A BCH
// codeword is valid exactly when this is zero.
func polyRemainder(v, gen int) int {
	degree := bitLen(gen) - 1
	for bitLen(v) > degree {
		v ^= gen << (bitLen(v) - degree - 1)
	}
	return v
}

// reservedGrid marks the function modules of a version-v symbol: everything
// that is not free to carry data. Built from the specification's rectangles,
// independently of how the encoder draws them.
func reservedGrid(version int) [][]bool {
	size := symbolSize(version)
	grid := make([][]bool, size)
	for i := range grid {
		grid[i] = make([]bool, size)
	}
	mark := func(row, col, height, width int) {
		for r := row; r < row+height; r++ {
			for c := col; c < col+width; c++ {
				if r >= 0 && r < size && c >= 0 && c < size {
					grid[r][c] = true
				}
			}
		}
	}

	// The three finder patterns with their separators, plus the format
	// information that sits immediately beside each of them (and the dark
	// module, in the bottom-left block).
	mark(0, 0, 9, 9)
	mark(0, size-8, 9, 8)
	mark(size-8, 0, 8, 9)
	// Timing patterns; the parts inside the finder blocks are already marked.
	mark(6, 0, 1, size)
	mark(0, 6, size, 1)
	// Alignment patterns, minus the three that would sit on a finder.
	centres := alignmentCentres[version-1]
	if len(centres) > 0 {
		last := centres[len(centres)-1]
		for _, row := range centres {
			for _, col := range centres {
				onFinder := (row == 6 && col == 6) ||
					(row == 6 && col == last) ||
					(row == last && col == 6)
				if !onFinder {
					mark(row-2, col-2, 5, 5)
				}
			}
		}
	}
	// Version information, from version 7 onwards.
	if version >= 7 {
		mark(0, size-11, 6, 3)
		mark(size-11, 0, 3, 6)
	}
	return grid
}

// formatFieldCopies reads the two copies of the 15-bit format information,
// least significant bit first in each case.
func formatFieldCopies(modules [][]bool) (first, second int) {
	size := len(modules)
	bit := func(row, col int) int {
		if modules[row][col] {
			return 1
		}
		return 0
	}
	for i := range 6 {
		first |= bit(i, 8) << i
	}
	first |= bit(7, 8) << 6
	first |= bit(8, 8) << 7
	first |= bit(8, 7) << 8
	for i := 9; i < 15; i++ {
		first |= bit(8, 14-i) << i
	}
	for i := range 8 {
		second |= bit(8, size-1-i) << i
	}
	for i := 8; i < 15; i++ {
		second |= bit(size-15+i, 8) << i
	}
	return first, second
}

// versionFieldCopies reads the two copies of the 18-bit version information.
func versionFieldCopies(modules [][]bool) (first, second int) {
	size := len(modules)
	bit := func(row, col int) int {
		if modules[row][col] {
			return 1
		}
		return 0
	}
	for i := range 18 {
		row, col := i/3, size-11+i%3
		first |= bit(row, col) << i
		second |= bit(col, row) << i
	}
	return first, second
}

// readRawCodewords walks the data modules in placement order, undoing the
// mask, and returns the first n codewords.
func readRawCodewords(modules, reserved [][]bool, mask, n int) []byte {
	size := len(modules)
	out := make([]byte, n)
	bit, totalBits := 0, n*8
	upward := true
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for i := range size {
			row := i
			if upward {
				row = size - 1 - i
			}
			for j := range 2 {
				col := right - j
				if reserved[row][col] || bit >= totalBits {
					continue
				}
				if modules[row][col] != maskDark(mask, row, col) {
					out[bit/8] |= 1 << (7 - bit%8)
				}
				bit++
			}
		}
		upward = !upward
	}
	return out
}

// deinterleave undoes interleave: it splits the codeword stream back into the
// per-block data and error-correction runs.
func deinterleave(raw []byte, version int, level ECLevel) (data, ec [][]byte) {
	spec := specFor(version, level)
	dataTotal := dataCodewords(version, level)
	shortLen := dataTotal / spec.numBlocks
	numLong := dataTotal % spec.numBlocks

	data = make([][]byte, spec.numBlocks)
	ec = make([][]byte, spec.numBlocks)
	blockLen := make([]int, spec.numBlocks)
	for i := range spec.numBlocks {
		blockLen[i] = shortLen
		if i >= spec.numBlocks-numLong {
			blockLen[i]++
		}
	}

	pos := 0
	for i := 0; i <= shortLen; i++ {
		for b := range spec.numBlocks {
			if i < blockLen[b] {
				data[b] = append(data[b], raw[pos])
				pos++
			}
		}
	}
	for range spec.ecPerBlock {
		for b := range spec.numBlocks {
			ec[b] = append(ec[b], raw[pos])
			pos++
		}
	}
	return data, ec
}

// rsSyndromes evaluates a codeword polynomial at the generator's roots,
// α^0 to α^(ecLen-1). A codeword a decoder would accept without correction
// has every syndrome zero.
func rsSyndromes(codeword []byte, ecLen int) []byte {
	out := make([]byte, ecLen)
	for i := range ecLen {
		var acc byte
		for _, c := range codeword {
			acc = gfMul(acc, gfExp[i]) ^ c
		}
		out[i] = acc
	}
	return out
}

// bitReader reads a big-endian bit stream out of a codeword run.
type bitReader struct {
	data []byte
	pos  int
}

func (r *bitReader) read(n int) (int, error) {
	if r.pos+n > len(r.data)*8 {
		return 0, fmt.Errorf("bit stream exhausted: want %d bits at offset %d of %d", n, r.pos, len(r.data)*8)
	}
	v := 0
	for range n {
		v = v<<1 | int(r.data[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v, nil
}

// decodeSymbol reads a symbol back to the content that was encoded into it.
func decodeSymbol(modules [][]bool) (decodedSymbol, error) {
	var out decodedSymbol
	size := len(modules)
	if size < 21 || size%4 != 1 {
		return out, fmt.Errorf("symbol is %d modules per side, which is not a valid size", size)
	}
	out.version = (size - 17) / 4
	if out.version < minVersion || out.version > maxVersion {
		return out, fmt.Errorf("symbol claims version %d", out.version)
	}

	first, second := formatFieldCopies(modules)
	if first != second {
		return out, fmt.Errorf("format copies disagree: %015b and %015b", first, second)
	}
	field := first ^ formatXOR
	if rem := polyRemainder(field, bchFormat); rem != 0 {
		return out, fmt.Errorf("format field %015b fails its BCH check (remainder %b)", first, rem)
	}
	switch indicator := field >> 13; indicator {
	case 0b01:
		out.level = ECLow
	case 0b00:
		out.level = ECMedium
	default:
		return out, fmt.Errorf("format field carries unsupported level indicator %02b", indicator)
	}
	out.mask = field >> 10 & 0b111

	if out.version >= 7 {
		firstV, secondV := versionFieldCopies(modules)
		if firstV != secondV {
			return out, fmt.Errorf("version copies disagree: %018b and %018b", firstV, secondV)
		}
		if rem := polyRemainder(firstV, bchVersion); rem != 0 {
			return out, fmt.Errorf("version field %018b fails its BCH check (remainder %b)", firstV, rem)
		}
		if got := firstV >> 12; got != out.version {
			return out, fmt.Errorf("version field says version %d, symbol size says %d", got, out.version)
		}
	}

	raw := readRawCodewords(modules, reservedGrid(out.version), out.mask, totalCodewords[out.version-1])
	dataBlocks, ecBlocks := deinterleave(raw, out.version, out.level)
	var stream []byte
	for i, block := range dataBlocks {
		codeword := append(append([]byte{}, block...), ecBlocks[i]...)
		for _, s := range rsSyndromes(codeword, len(ecBlocks[i])) {
			if s != 0 {
				return out, fmt.Errorf("block %d fails its Reed-Solomon check", i)
			}
		}
		stream = append(stream, block...)
	}

	reader := &bitReader{data: stream}
	mode, err := reader.read(4)
	if err != nil {
		return out, fmt.Errorf("read mode indicator: %w", err)
	}
	if mode != byteMode {
		return out, fmt.Errorf("mode indicator is %04b, want %04b (byte mode)", mode, byteMode)
	}
	length, err := reader.read(charCountBits(out.version))
	if err != nil {
		return out, fmt.Errorf("read character count: %w", err)
	}
	content := make([]byte, length)
	for i := range length {
		b, err := reader.read(8)
		if err != nil {
			return out, fmt.Errorf("read content byte %d of %d: %w", i, length, err)
		}
		content[i] = byte(b)
	}
	out.content = string(content)
	return out, nil
}

// modulesOf copies a code's modules into a plain grid.
func modulesOf(c *Code) [][]bool {
	grid := make([][]bool, c.Size())
	for row := range c.Size() {
		grid[row] = make([]bool, c.Size())
		for col := range c.Size() {
			grid[row][col] = c.Module(row, col)
		}
	}
	return grid
}

// TestReservedLayoutMatchesEncoder pins the decoder's independently built
// function-module map against the one the encoder reserves. If these ever
// disagree, one of the two has the symbol geometry wrong.
func TestReservedLayoutMatchesEncoder(t *testing.T) {
	for v := minVersion; v <= maxVersion; v++ {
		want := newMatrix(v, ECMedium).fn
		got := reservedGrid(v)
		for row := range want {
			for col := range want[row] {
				if got[row][col] != want[row][col] {
					t.Fatalf("version %d: reserved(%d, %d) = %v, encoder says %v",
						v, row, col, got[row][col], want[row][col])
				}
			}
		}
	}
}

// TestSymbolDecodesBackToContent is the end-to-end check: every symbol this
// encoder produces must read back, through an independent decoder, to exactly
// the string that went in — with valid error correction and a readable format
// field.
func TestSymbolDecodesBackToContent(t *testing.T) {
	pairURL, err := offerFixture().URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}

	tests := []struct {
		name    string
		content string
		level   ECLevel
	}{
		{name: "pairing offer URL", content: pairURL, level: ECMedium},
		{name: "pairing offer URL at L", content: pairURL, level: ECLow},
		{name: "empty", content: "", level: ECMedium},
		{name: "single byte", content: "x", level: ECMedium},
		{name: "version 1 full at L", content: strings.Repeat("a", capacityBytes(1, ECLow)), level: ECLow},
		{name: "spans the character-count widening", content: strings.Repeat("b", 260), level: ECMedium},
		{name: "largest supported at M", content: strings.Repeat("c", capacityBytes(maxVersion, ECMedium)), level: ECMedium},
		{name: "largest supported at L", content: strings.Repeat("d", capacityBytes(maxVersion, ECLow)), level: ECLow},
		{name: "non-ASCII bytes", content: "pair with «smind» — daemon ✻ ready", level: ECMedium},
		{name: "every byte value", content: allByteValues(), level: ECMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := EncodeQR(tt.content, tt.level)
			if err != nil {
				t.Fatalf("EncodeQR() error = %v", err)
			}
			got, err := decodeSymbol(modulesOf(code))
			if err != nil {
				t.Fatalf("decodeSymbol() error = %v", err)
			}
			if got.content != tt.content {
				t.Errorf("decoded %q, want %q", got.content, tt.content)
			}
			if got.version != code.Version() {
				t.Errorf("decoded version %d, want %d", got.version, code.Version())
			}
			if got.level != tt.level {
				t.Errorf("decoded level %s, want %s", got.level, tt.level)
			}
			if got.mask != code.Mask() {
				t.Errorf("decoded mask %d, want %d", got.mask, code.Mask())
			}
		})
	}
}

// allByteValues returns a string containing every byte from 0 to 255, so the
// encoder's 8-bit byte mode is exercised over its whole alphabet.
func allByteValues() string {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}

// TestPNGDecodesBackToContent runs the decoder over pixels rather than over
// the in-memory grid, so the rendered image — not just the module matrix — is
// what gets read back.
func TestPNGDecodesBackToContent(t *testing.T) {
	pairURL, err := offerFixture().URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}
	const moduleSize = 5

	code, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	raw, err := QRPNG(pairURL, moduleSize)
	if err != nil {
		t.Fatalf("QRPNG() error = %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}

	dark := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return (r+g+b)/3 < 0x8000
	}
	// The quiet zone has to be blank, or a scanner cannot find the symbol's
	// edges. Check the outermost ring of pixels.
	side := img.Bounds().Dx()
	for i := range side {
		for _, p := range [][2]int{{i, 0}, {i, side - 1}, {0, i}, {side - 1, i}} {
			if dark(p[0], p[1]) {
				t.Fatalf("pixel (%d, %d) in the quiet zone is dark", p[0], p[1])
			}
		}
	}

	size := code.Size()
	modules := make([][]bool, size)
	for row := range size {
		modules[row] = make([]bool, size)
		for col := range size {
			x := (col+quietZoneModules)*moduleSize + moduleSize/2
			y := (row+quietZoneModules)*moduleSize + moduleSize/2
			modules[row][col] = dark(x, y)
		}
	}

	got, err := decodeSymbol(modules)
	if err != nil {
		t.Fatalf("decodeSymbol() over PNG pixels error = %v", err)
	}
	if got.content != pairURL {
		t.Errorf("decoded %q, want %q", got.content, pairURL)
	}
}

// TestTextDecodesBackToContent reads the terminal rendering back. Code.Text
// draws *light* modules as the filled glyphs, so this inverts that convention
// on the way in; a scanner pointed at a dark-background terminal sees the same
// thing.
func TestTextDecodesBackToContent(t *testing.T) {
	pairURL, err := offerFixture().URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}
	code, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	text, err := QRText(pairURL)
	if err != nil {
		t.Fatalf("QRText() error = %v", err)
	}

	size := code.Size()
	modules := make([][]bool, size)
	for row := range modules {
		modules[row] = make([]bool, size)
	}
	for lineNo, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		for col, glyph := range []rune(line) {
			upperDark := glyph == ' ' || glyph == '▄'
			lowerDark := glyph == ' ' || glyph == '▀'
			for i, dark := range []bool{upperDark, lowerDark} {
				row := 2*lineNo + i - quietZoneModules
				c := col - quietZoneModules
				if row < 0 || row >= size || c < 0 || c >= size {
					if dark {
						t.Fatalf("quiet zone at text line %d column %d is dark", lineNo, col)
					}
					continue
				}
				modules[row][c] = dark
			}
		}
	}

	got, err := decodeSymbol(modules)
	if err != nil {
		t.Fatalf("decodeSymbol() over the text rendering error = %v", err)
	}
	if got.content != pairURL {
		t.Errorf("decoded %q, want %q", got.content, pairURL)
	}
}

// TestDecoderRejectsDamage checks the decoder actually has teeth: a flipped
// data module must fail the Reed-Solomon check, and a flipped format module
// must fail the BCH check. Without this, a decoder that accepted anything
// would make the round-trip tests above vacuous.
func TestDecoderRejectsDamage(t *testing.T) {
	code, err := EncodeQR("https://spacingmind.sh/pair#offer=abcdefghijklmnop", ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}

	t.Run("flipped data module", func(t *testing.T) {
		modules := modulesOf(code)
		reserved := reservedGrid(code.Version())
		flipped := false
		for row := range modules {
			for col := range modules[row] {
				if !reserved[row][col] && !flipped {
					modules[row][col] = !modules[row][col]
					flipped = true
				}
			}
		}
		if !flipped {
			t.Fatal("found no data module to damage")
		}
		if _, err := decodeSymbol(modules); err == nil {
			t.Error("decodeSymbol() accepted a symbol with a flipped data module")
		}
	})

	t.Run("flipped format module", func(t *testing.T) {
		modules := modulesOf(code)
		// Row 8, column 0 is a format information module in both copies' sense:
		// it carries bit 14 of the first copy.
		modules[8][0] = !modules[8][0]
		if _, err := decodeSymbol(modules); err == nil {
			t.Error("decodeSymbol() accepted a symbol with a flipped format module")
		}
	})
}

// TestOfferQRScansBackToTheOffer is the pairing acceptance criterion end to
// end: an offer renders itself as a QR code, and reading that code back the
// way a phone's camera would yields the deep link, whose fragment parses to
// the same offer that went in.
func TestOfferQRScansBackToTheOffer(t *testing.T) {
	bases := []struct {
		name string
		base string
	}{
		{name: "default pair URL", base: ""},
		{name: "custom base", base: "https://pair.example.test/join"},
	}
	for _, tt := range bases {
		t.Run(tt.name, func(t *testing.T) {
			offer := testOffer(t)
			code, err := offer.QRCode(tt.base)
			if err != nil {
				t.Fatalf("Offer.QRCode() error = %v", err)
			}
			scanned, err := decodeSymbol(modulesOf(code))
			if err != nil {
				t.Fatalf("decodeSymbol() error = %v", err)
			}

			// What the scanner read has to be the deep link, with the payload in
			// the fragment — the whole point of the fragment encoding is that a
			// scan never puts the offer anywhere a server could log it.
			u, err := url.Parse(scanned.content)
			if err != nil {
				t.Fatalf("scanned content %q does not parse as a URL: %v", scanned.content, err)
			}
			if u.RawQuery != "" {
				t.Errorf("scanned URL has query string %q, want none", u.RawQuery)
			}
			if !strings.HasPrefix(u.Fragment, FragmentKey+"=") {
				t.Errorf("scanned URL fragment = %q, want it to start with %q=", u.Fragment, FragmentKey)
			}

			got, err := ParseURL(scanned.content)
			if err != nil {
				t.Fatalf("ParseURL() on the scanned link error = %v", err)
			}
			if got.DaemonID != offer.DaemonID {
				t.Errorf("DaemonID = %q, want %q", got.DaemonID, offer.DaemonID)
			}
			if !bytes.Equal(got.PublicKey, offer.PublicKey) {
				t.Errorf("PublicKey = %x, want %x", got.PublicKey, offer.PublicKey)
			}
			if got.Relay != offer.Relay {
				t.Errorf("Relay = %q, want %q", got.Relay, offer.Relay)
			}
			if got.RelayFingerprint != offer.RelayFingerprint {
				t.Errorf("RelayFingerprint = %q, want %q", got.RelayFingerprint, offer.RelayFingerprint)
			}
		})
	}
}

// TestOfferRenderersMatchTheURLHelpers checks the offer-level renderers are
// the URL-level ones applied to the offer's own deep link, rather than a
// second rendering path that could drift from it.
func TestOfferRenderersMatchTheURLHelpers(t *testing.T) {
	offer := testOffer(t)
	pairURL, err := offer.URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}
	const moduleSize = 6

	wantPNG, err := QRPNG(pairURL, moduleSize)
	if err != nil {
		t.Fatalf("QRPNG() error = %v", err)
	}
	gotPNG, err := offer.QRPNG("", moduleSize)
	if err != nil {
		t.Fatalf("Offer.QRPNG() error = %v", err)
	}
	if !bytes.Equal(gotPNG, wantPNG) {
		t.Errorf("Offer.QRPNG() is %d bytes, QRPNG() of the same link is %d", len(gotPNG), len(wantPNG))
	}

	wantText, err := QRText(pairURL)
	if err != nil {
		t.Fatalf("QRText() error = %v", err)
	}
	gotText, err := offer.QRText("")
	if err != nil {
		t.Fatalf("Offer.QRText() error = %v", err)
	}
	if gotText != wantText {
		t.Error("Offer.QRText() differs from QRText() of the same link")
	}
}

// TestOfferQRRejectsUnrenderableOffers checks an offer that cannot be paired
// with fails at render time rather than producing a QR code that leads
// nowhere.
func TestOfferQRRejectsUnrenderableOffers(t *testing.T) {
	var incomplete Offer
	if _, err := incomplete.QRCode(""); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("QRCode() on an empty offer = %v, want ErrInvalidOffer", err)
	}
	if _, err := incomplete.QRPNG("", 4); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("QRPNG() on an empty offer = %v, want ErrInvalidOffer", err)
	}
	if _, err := incomplete.QRText(""); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("QRText() on an empty offer = %v, want ErrInvalidOffer", err)
	}

	offer := testOffer(t)
	if _, err := offer.QRCode("https://pair.example.test/join#already=set"); !errors.Is(err, ErrInvalidOffer) {
		t.Errorf("QRCode() with a base that already has a fragment = %v, want ErrInvalidOffer", err)
	}
	if _, err := offer.QRPNG("", 0); err == nil {
		t.Error("QRPNG() with a zero module size = nil error, want an error")
	}
}
