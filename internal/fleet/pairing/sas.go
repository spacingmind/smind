package pairing

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// sasDomain separates the fleet SAS from every other SHA-256 use in the
// daemon.
const sasDomain = "smind-fleet-sas-v1"

// SAS derives the six-digit safety number both humans compare after the
// handshake: a hash over both host public keys and both handshake nonces,
// so it changes if either endpoint's identity or either side of the
// exchange is swapped. Length-prefixed fields keep the inputs from being
// re-cut into a different split.
func SAS(callerPub, calleePub, nonceC, nonceA []byte) string {
	h := sha256.New()
	h.Write([]byte(sasDomain))
	for _, b := range [][]byte{callerPub, calleePub, nonceC, nonceA} {
		var lenPrefix [2]byte
		binary.BigEndian.PutUint16(lenPrefix[:], uint16(len(b)))
		h.Write(lenPrefix[:])
		h.Write(b)
	}
	sum := h.Sum(nil)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(sum[:4])%1000000)
}
