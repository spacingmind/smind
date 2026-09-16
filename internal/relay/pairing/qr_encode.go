package pairing

import "fmt"

// Version limits. Only the versions a pairing deep link can possibly need are
// supported; version 15 at level M holds 412 bytes, roughly twice the size of
// the longest offer URL this package produces.
const (
	minVersion = 1
	maxVersion = 15
)

// byteMode is the 4-bit mode indicator for 8-bit byte mode.
const byteMode = 0b0100

// padBytes are the alternating pad codewords appended after the terminator.
var padBytes = [2]byte{0xEC, 0x11}

// totalCodewords[v-1] is the number of data plus error-correction codewords in
// a version-v symbol.
var totalCodewords = [maxVersion]int{
	26, 44, 70, 100, 134, 172, 196, 242, 292, 346, 404, 466, 532, 581, 655,
}

// blockSpec describes the error-correction block layout of one (version,
// level) pair: how many EC codewords each block carries and how many blocks
// the data is split into.
type blockSpec struct {
	ecPerBlock int
	numBlocks  int
}

// ecSpecs[v-1][level] is the block layout for version v at that level.
var ecSpecs = [maxVersion][2]blockSpec{
	{{7, 1}, {10, 1}},
	{{10, 1}, {16, 1}},
	{{15, 1}, {26, 1}},
	{{20, 1}, {18, 2}},
	{{26, 1}, {24, 2}},
	{{18, 2}, {16, 4}},
	{{20, 2}, {18, 4}},
	{{24, 2}, {22, 4}},
	{{30, 2}, {22, 5}},
	{{18, 4}, {26, 5}},
	{{20, 4}, {30, 5}},
	{{24, 4}, {22, 8}},
	{{26, 4}, {22, 9}},
	{{30, 4}, {24, 9}},
	{{22, 6}, {24, 10}},
}

// specFor returns the block layout for a version and level.
func specFor(version int, level ECLevel) blockSpec {
	return ecSpecs[version-1][level]
}

// dataCodewords returns how many of a symbol's codewords carry data rather
// than error correction.
func dataCodewords(version int, level ECLevel) int {
	spec := specFor(version, level)
	return totalCodewords[version-1] - spec.ecPerBlock*spec.numBlocks
}

// charCountBits returns the width of the byte-mode character-count field,
// which widens at version 10.
func charCountBits(version int) int {
	if version < 10 {
		return 8
	}
	return 16
}

// capacityBytes returns how many content bytes fit in a symbol of this version
// and level, after the mode indicator and character count.
func capacityBytes(version int, level ECLevel) int {
	bits := dataCodewords(version, level)*8 - 4 - charCountBits(version)
	if bits < 0 {
		return 0
	}
	return bits / 8
}

// pickVersion returns the smallest supported version that holds n content
// bytes at the given level.
func pickVersion(n int, level ECLevel) (int, error) {
	for v := minVersion; v <= maxVersion; v++ {
		if capacityBytes(v, level) >= n {
			return v, nil
		}
	}
	return 0, fmt.Errorf("%w: %d bytes exceeds the %d-byte capacity of a version %d level %s symbol",
		ErrContentTooLong, n, capacityBytes(maxVersion, level), maxVersion, level)
}

// bitBuffer accumulates a big-endian bit stream.
type bitBuffer struct {
	bytes []byte
	bits  int
}

// len returns the number of bits written so far.
func (b *bitBuffer) len() int { return b.bits }

// appendBit appends a single bit.
func (b *bitBuffer) appendBit(set bool) {
	if b.bits%8 == 0 {
		b.bytes = append(b.bytes, 0)
	}
	if set {
		b.bytes[b.bits/8] |= 1 << (7 - b.bits%8)
	}
	b.bits++
}

// appendBits appends the low n bits of value, most significant first.
func (b *bitBuffer) appendBits(value, n int) {
	for i := n - 1; i >= 0; i-- {
		b.appendBit(value>>i&1 == 1)
	}
}

// appendByte appends all eight bits of a byte.
func (b *bitBuffer) appendByte(v byte) { b.appendBits(int(v), 8) }

// encodeData turns content into the full run of data codewords for a symbol of
// this version and level: mode indicator, character count, the bytes
// themselves, a terminator, and pad codewords out to the symbol's capacity.
func encodeData(content string, version int, level ECLevel) []byte {
	total := dataCodewords(version, level)
	capacity := total * 8

	var buf bitBuffer
	buf.appendBits(byteMode, 4)
	buf.appendBits(len(content), charCountBits(version))
	for i := range len(content) {
		buf.appendByte(content[i])
	}

	// Terminator: up to four zero bits, truncated if the symbol is full.
	for range min(4, capacity-buf.len()) {
		buf.appendBit(false)
	}
	// Pad to a byte boundary, then alternate pad codewords.
	for buf.len()%8 != 0 {
		buf.appendBit(false)
	}
	out := buf.bytes
	for i := 0; len(out) < total; i++ {
		out = append(out, padBytes[i%len(padBytes)])
	}
	return out
}

// interleave splits data into the symbol's error-correction blocks, computes
// each block's EC codewords, and returns the interleaved codeword sequence in
// the order it gets written into the symbol.
//
// Block sizes are derived rather than tabulated: every block holds either
// shortLen or shortLen+1 data codewords, and the longer blocks come last.
func interleave(data []byte, version int, level ECLevel) []byte {
	spec := specFor(version, level)
	shortLen := len(data) / spec.numBlocks
	numLong := len(data) % spec.numBlocks

	dataBlocks := make([][]byte, spec.numBlocks)
	ecBlocks := make([][]byte, spec.numBlocks)
	off := 0
	for i := range spec.numBlocks {
		n := shortLen
		if i >= spec.numBlocks-numLong {
			n++
		}
		dataBlocks[i] = data[off : off+n]
		ecBlocks[i] = rsEncode(dataBlocks[i], spec.ecPerBlock)
		off += n
	}

	out := make([]byte, 0, len(data)+spec.ecPerBlock*spec.numBlocks)
	for i := 0; i <= shortLen; i++ {
		for _, block := range dataBlocks {
			if i < len(block) {
				out = append(out, block[i])
			}
		}
	}
	for i := range spec.ecPerBlock {
		for _, block := range ecBlocks {
			out = append(out, block[i])
		}
	}
	return out
}
