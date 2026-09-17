package pairing

// Reed-Solomon error correction over GF(256), as used by QR codes.
//
// The field is built from the primitive polynomial x^8 + x^4 + x^3 + x^2 + 1
// (0x11D) with 2 as the generator element, which is what ISO/IEC 18004
// specifies.

// gfPrimitive is the primitive polynomial of the QR Galois field.
const gfPrimitive = 0x11D

// gfExp maps an exponent to α^exponent; it is doubled in length so that
// exponent sums up to 508 can be looked up without a modulo. gfLog is its
// inverse (gfLog[0] is unused).
var gfExp, gfLog = newGaloisTables()

func newGaloisTables() ([512]byte, [256]byte) {
	var exp [512]byte
	var log [256]byte
	x := 1
	for i := range 255 {
		exp[i] = byte(x)
		log[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= gfPrimitive
		}
	}
	for i := 255; i < len(exp); i++ {
		exp[i] = exp[i-255]
	}
	return exp, log
}

// gfMul multiplies two field elements.
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// rsGenerator returns the Reed-Solomon generator polynomial of the given
// degree, (x-α^0)(x-α^1)...(x-α^(degree-1)), in descending coefficient order
// with a leading coefficient of 1.
func rsGenerator(degree int) []byte {
	poly := []byte{1}
	for i := range degree {
		next := make([]byte, len(poly)+1)
		for j, c := range poly {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		poly = next
	}
	return poly
}

// rsEncode returns the ecLen error-correction codewords for data: the
// remainder of data*x^ecLen divided by the generator polynomial.
func rsEncode(data []byte, ecLen int) []byte {
	if ecLen <= 0 {
		return nil
	}
	gen := rsGenerator(ecLen)[1:] // the leading 1 is implicit below
	rem := make([]byte, ecLen)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[ecLen-1] = 0
		for i, g := range gen {
			rem[i] ^= gfMul(g, factor)
		}
	}
	return rem
}
