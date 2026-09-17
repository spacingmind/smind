package pairing

import (
	"bytes"
	"errors"
	"image/png"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestReedSolomonKnownVector checks the GF(256) arithmetic and generator
// polynomial against the worked "HELLO WORLD" version 1-M example from the
// QR specification.
func TestReedSolomonKnownVector(t *testing.T) {
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}

	got := rsEncode(data, len(want))
	if !bytes.Equal(got, want) {
		t.Errorf("rsEncode() = %v, want %v", got, want)
	}
}

// TestTotalCodewordsMatchGeometry derives the codeword count of every version
// from the module layout — total modules minus function patterns, divided by
// eight — and checks it against the published totals. Because the alignment
// patterns are the only part of the layout that varies in an irregular way,
// this validates the alignment coordinate table as a side effect.
func TestTotalCodewordsMatchGeometry(t *testing.T) {
	want := [maxVersion]int{26, 44, 70, 100, 134, 172, 196, 242, 292, 346, 404, 466, 532, 581, 655}
	for v := minVersion; v <= maxVersion; v++ {
		free := newMatrix(v, ECLow).freeModules()
		if got := free / 8; got != want[v-1] {
			t.Errorf("version %d: %d free modules => %d codewords, want %d", v, free, got, want[v-1])
		}
		if totalCodewords[v-1] != want[v-1] {
			t.Errorf("version %d: totalCodewords = %d, want %d", v, totalCodewords[v-1], want[v-1])
		}
	}
}

// TestByteModeCapacity checks the computed content capacity of every
// supported version and level against the published byte-mode capacities.
func TestByteModeCapacity(t *testing.T) {
	want := [maxVersion][2]int{
		{17, 14}, {32, 26}, {53, 42}, {78, 62}, {106, 84},
		{134, 106}, {154, 122}, {192, 152}, {230, 180}, {271, 213},
		{321, 251}, {367, 287}, {425, 331}, {458, 362}, {520, 412},
	}
	for v := minVersion; v <= maxVersion; v++ {
		for _, level := range []ECLevel{ECLow, ECMedium} {
			got := capacityBytes(v, level)
			if exp := want[v-1][level]; got != exp {
				t.Errorf("capacityBytes(%d, %s) = %d, want %d", v, level, got, exp)
			}
		}
	}
}

// TestSymbolStructure checks the immovable parts of a symbol: its size, the
// three finder patterns with their separators, the timing patterns and the
// dark module.
func TestSymbolStructure(t *testing.T) {
	for v := minVersion; v <= maxVersion; v++ {
		content := strings.Repeat("x", capacityBytes(v, ECMedium))
		code, err := EncodeQR(content, ECMedium)
		if err != nil {
			t.Fatalf("version %d: EncodeQR() error = %v", v, err)
		}
		if code.Version() != v {
			t.Fatalf("content of %d bytes encoded at version %d, want %d", len(content), code.Version(), v)
		}
		size := code.Size()
		if want := 17 + 4*v; size != want {
			t.Errorf("version %d: Size() = %d, want %d", v, size, want)
		}

		// Finder patterns and the light separator band around them.
		for _, corner := range [3][2]int{{0, 0}, {0, size - 7}, {size - 7, 0}} {
			for dr := -1; dr <= 7; dr++ {
				for dc := -1; dc <= 7; dc++ {
					row, col := corner[0]+dr, corner[1]+dc
					if row < 0 || row >= size || col < 0 || col >= size {
						continue
					}
					if got, want := code.Module(row, col), finderDark(dr, dc); got != want {
						t.Errorf("version %d: module(%d, %d) = %v, want %v (finder)", v, row, col, got, want)
					}
				}
			}
		}

		// Timing patterns alternate, starting dark at the even coordinates.
		for i := 8; i < size-8; i++ {
			want := i%2 == 0
			if got := code.Module(6, i); got != want {
				t.Errorf("version %d: horizontal timing module(6, %d) = %v, want %v", v, i, got, want)
			}
			if got := code.Module(i, 6); got != want {
				t.Errorf("version %d: vertical timing module(%d, 6) = %v, want %v", v, i, got, want)
			}
		}

		if !code.Module(4*v+9, 8) {
			t.Errorf("version %d: dark module at (%d, 8) is light", v, 4*v+9)
		}
	}
}

// TestFinderPatternShape pins the 7x7 finder pattern itself, so a change to
// finderDark cannot quietly redefine what TestSymbolStructure compares to.
func TestFinderPatternShape(t *testing.T) {
	want := []string{
		"#######",
		"#     #",
		"# ### #",
		"# ### #",
		"# ### #",
		"#     #",
		"#######",
	}
	for dr, row := range want {
		for dc, c := range row {
			if got := finderDark(dr, dc); got != (c == '#') {
				t.Errorf("finderDark(%d, %d) = %v, want %v", dr, dc, got, c == '#')
			}
		}
	}
	if finderDark(-1, 3) || finderDark(3, 7) {
		t.Error("separator modules must be light")
	}
}

// TestFormatInformation checks the BCH(15, 5) format field, and with it the
// level indicators, against published format information strings.
func TestFormatInformation(t *testing.T) {
	tests := []struct {
		level ECLevel
		mask  int
		want  int
	}{
		{level: ECLow, mask: 0, want: 0b111011111000100},
		{level: ECLow, mask: 5, want: 0b110001100011000},
		{level: ECMedium, mask: 0, want: 0b101010000010010},
	}
	for _, tt := range tests {
		if got := formatBits(tt.level, tt.mask); got != tt.want {
			t.Errorf("formatBits(%s, %d) = %015b, want %015b", tt.level, tt.mask, got, tt.want)
		}
	}
	// All 16 format fields must be distinct, or a scanner could not tell the
	// level and mask apart.
	seen := map[int]string{}
	for _, level := range []ECLevel{ECLow, ECMedium} {
		for mask := range numMasks {
			bits := formatBits(level, mask)
			key := level.String() + string(rune('0'+mask))
			if prev, dup := seen[bits]; dup {
				t.Errorf("format field %015b used by both %s and %s", bits, prev, key)
			}
			seen[bits] = key
		}
	}
}

// TestVersionInformation checks the BCH(18, 6) version field against the
// published string for version 7.
func TestVersionInformation(t *testing.T) {
	if got, want := versionBits(7), 0b000111110010010100; got != want {
		t.Errorf("versionBits(7) = %018b, want %018b", got, want)
	}
	// The field must be present exactly from version 7 upwards, and its low
	// six bits always spell out the version.
	for v := 7; v <= maxVersion; v++ {
		if got := versionBits(v) >> 12; got != v {
			t.Errorf("versionBits(%d) carries version %d", v, got)
		}
	}
}

// TestVersionSelection checks that the smallest version that fits is chosen
// and that oversized content is rejected rather than panicking.
func TestVersionSelection(t *testing.T) {
	tests := []struct {
		name    string
		bytes   int
		level   ECLevel
		version int
	}{
		{name: "fills version 1 at L", bytes: 17, level: ECLow, version: 1},
		{name: "one byte over version 1 at L", bytes: 18, level: ECLow, version: 2},
		{name: "twenty bytes at L", bytes: 20, level: ECLow, version: 2},
		{name: "twenty bytes at M", bytes: 20, level: ECMedium, version: 2},
		{name: "empty content", bytes: 0, level: ECMedium, version: 1},
		{name: "pairing-sized URL at M", bytes: 205, level: ECMedium, version: 10},
		{name: "largest supported at M", bytes: 412, level: ECMedium, version: 15},
		{name: "largest supported at L", bytes: 520, level: ECLow, version: 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := EncodeQR(strings.Repeat("a", tt.bytes), tt.level)
			if err != nil {
				t.Fatalf("EncodeQR() error = %v", err)
			}
			if code.Version() != tt.version {
				t.Errorf("%d bytes at %s: version = %d, want %d", tt.bytes, tt.level, code.Version(), tt.version)
			}
			if code.Level() != tt.level {
				t.Errorf("Level() = %s, want %s", code.Level(), tt.level)
			}
			if code.Mask() < 0 || code.Mask() >= numMasks {
				t.Errorf("Mask() = %d, want 0..%d", code.Mask(), numMasks-1)
			}
		})
	}
}

func TestEncodeQRContentTooLong(t *testing.T) {
	for _, level := range []ECLevel{ECLow, ECMedium} {
		over := capacityBytes(maxVersion, level) + 1
		code, err := EncodeQR(strings.Repeat("a", over), level)
		if !errors.Is(err, ErrContentTooLong) {
			t.Errorf("%d bytes at %s: error = %v, want ErrContentTooLong", over, level, err)
		}
		if code != nil {
			t.Errorf("%d bytes at %s: got a code back alongside the error", over, level)
		}
	}
}

// TestEncodeQRUnsupportedLevel guards the level switch, since the format
// indicator silently treats anything that is not L as M.
func TestEncodeQRUnsupportedLevel(t *testing.T) {
	if _, err := EncodeQR("hello", ECLevel(9)); err == nil {
		t.Error("EncodeQR() with an unknown level = nil error, want an error")
	}
}

// offerFixture builds a representative pairing offer.
func offerFixture() Offer {
	pk := make([]byte, 32)
	for i := range pk {
		pk[i] = byte(i + 1)
	}
	return Offer{
		DaemonID:         "daemon-01JB7Q0Z3M5F7K9V2X4C6H8N1P",
		PublicKey:        pk,
		Relay:            "wss://relay.spacingmind.sh/v1/connect",
		RelayFingerprint: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}
}

// TestEncodePairingOffer runs the real pairing deep link through the encoder
// and both renderers.
func TestEncodePairingOffer(t *testing.T) {
	pairURL, err := offerFixture().URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}
	t.Logf("pairing URL is %d bytes", len(pairURL))

	code, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	if code.Version() < minVersion || code.Version() > maxVersion {
		t.Fatalf("Version() = %d, want %d..%d", code.Version(), minVersion, maxVersion)
	}
	if code.Size() != 17+4*code.Version() {
		t.Errorf("Size() = %d, want %d", code.Size(), 17+4*code.Version())
	}

	const moduleSize = 6
	raw, err := QRPNG(pairURL, moduleSize)
	if err != nil {
		t.Fatalf("QRPNG() error = %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	wantSide := (code.Size() + 2*quietZoneModules) * moduleSize
	if b := img.Bounds(); b.Dx() != wantSide || b.Dy() != wantSide {
		t.Errorf("PNG is %dx%d, want %dx%d", b.Dx(), b.Dy(), wantSide, wantSide)
	}

	text, err := QRText(pairURL)
	if err != nil {
		t.Fatalf("QRText() error = %v", err)
	}
	if text == "" {
		t.Fatal("QRText() returned an empty string")
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("QRText() output does not end in a newline")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	side := code.Size() + 2*quietZoneModules
	if want := (side + 1) / 2; len(lines) != want {
		t.Errorf("QRText() has %d lines, want %d", len(lines), want)
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != side {
			t.Errorf("QRText() line %d is %d runes wide, want %d", i, got, side)
		}
	}
}

// TestEncodeDeterministic checks that encoding is a pure function of its
// inputs: no map iteration order or randomness leaks into the modules.
func TestEncodeDeterministic(t *testing.T) {
	pairURL, err := offerFixture().URL("")
	if err != nil {
		t.Fatalf("Offer.URL() error = %v", err)
	}
	first, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	second, err := EncodeQR(pairURL, ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	if first.Version() != second.Version() || first.Mask() != second.Mask() {
		t.Fatalf("got version %d mask %d then version %d mask %d",
			first.Version(), first.Mask(), second.Version(), second.Mask())
	}
	for row := range first.Size() {
		for col := range first.Size() {
			if first.Module(row, col) != second.Module(row, col) {
				t.Fatalf("module(%d, %d) differs between encodings", row, col)
			}
		}
	}
}

// TestBlockSplit checks the derived block layout: every data codeword lands in
// exactly one block, and the long blocks are one codeword longer than the
// short ones.
func TestBlockSplit(t *testing.T) {
	for v := minVersion; v <= maxVersion; v++ {
		for _, level := range []ECLevel{ECLow, ECMedium} {
			spec := specFor(v, level)
			total := dataCodewords(v, level)
			shortLen := total / spec.numBlocks
			numLong := total % spec.numBlocks
			numShort := spec.numBlocks - numLong
			if got := numShort*shortLen + numLong*(shortLen+1); got != total {
				t.Errorf("version %d %s: blocks hold %d codewords, want %d", v, level, got, total)
			}
			if shortLen < 1 {
				t.Errorf("version %d %s: %d blocks for %d data codewords leaves empty blocks",
					v, level, spec.numBlocks, total)
			}
		}
	}
}

// TestCodewordsSurviveThePlacement reads the symbol back: it removes the mask,
// walks the data modules in placement order and de-interleaves nothing —
// comparing directly against the interleaved codewords that went in. This
// catches a mask that is applied to function modules, a format field written
// over data, or a traversal that drifts between writing and reading.
func TestCodewordsSurviveThePlacement(t *testing.T) {
	const content = "https://spacingmind.sh/pair#offer=abcdefghijklmnopqrstuvwxyz0123456789"
	for _, level := range []ECLevel{ECLow, ECMedium} {
		code, err := EncodeQR(content, level)
		if err != nil {
			t.Fatalf("EncodeQR() error = %v", err)
		}
		v := code.Version()
		want := interleave(encodeData(content, v, level), v, level)
		if got := readCodewords(code, len(want)); !bytes.Equal(got, want) {
			t.Errorf("level %s: read back %v, want %v", level, got, want)
		}
	}
}

// readCodewords is the inverse of matrix.placeData: it unmasks the symbol and
// collects the first n codewords from the data modules.
func readCodewords(c *Code, n int) []byte {
	fn := newMatrix(c.Version(), c.Level()).fn
	out := make([]byte, n)
	bit, totalBits := 0, n*8
	upward := true
	for right := c.Size() - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for i := range c.Size() {
			row := i
			if upward {
				row = c.Size() - 1 - i
			}
			for j := range 2 {
				col := right - j
				if fn[row][col] || bit >= totalBits {
					continue
				}
				if c.Module(row, col) != maskDark(c.Mask(), row, col) {
					out[bit/8] |= 1 << (7 - bit%8)
				}
				bit++
			}
		}
		upward = !upward
	}
	return out
}

// TestPNGRejectsBadGeometry checks the argument validation on PNG.
func TestPNGRejectsBadGeometry(t *testing.T) {
	code, err := EncodeQR("hello", ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	if _, err := code.PNG(0, 4); err == nil {
		t.Error("PNG() with a zero module size = nil error, want an error")
	}
	if _, err := code.PNG(1, -1); err == nil {
		t.Error("PNG() with a negative quiet zone = nil error, want an error")
	}
}

// TestModuleOutOfBounds checks that reading past the symbol edge is light
// rather than a panic, which is what lets Text draw its quiet zone.
func TestModuleOutOfBounds(t *testing.T) {
	code, err := EncodeQR("hello", ECMedium)
	if err != nil {
		t.Fatalf("EncodeQR() error = %v", err)
	}
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {code.Size(), 0}, {0, code.Size()}} {
		if code.Module(p[0], p[1]) {
			t.Errorf("Module(%d, %d) outside the symbol is dark", p[0], p[1])
		}
	}
}
