package selfupdate

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// ErrNoChecksum means checksums.txt has no entry for the asset.
var ErrNoChecksum = errors.New("no checksum listed")

// ErrChecksumMismatch means the downloaded archive's SHA-256 differs from the
// one in checksums.txt.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// ExpectedChecksum finds name's SHA-256 (lower-case hex) in a sha256sum-style
// checksums.txt: `<64 hex>  <name>` or `<64 hex> *<name>`. A name listed twice
// with different digests is an error rather than first-match-wins, so a
// tampered file cannot smuggle a second line past the check.
func ExpectedChecksum(checksums []byte, name string) (string, error) {
	var found string
	sc := bufio.NewScanner(bytes.NewReader(checksums))
	for sc.Scan() {
		sum, file, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		file = strings.TrimPrefix(strings.TrimSpace(file), "*")
		if file != name {
			continue
		}
		sum = strings.ToLower(sum)
		if !isSHA256Hex(sum) {
			return "", fmt.Errorf("malformed checksum for %s in checksums.txt", name)
		}
		if found != "" && found != sum {
			return "", fmt.Errorf("checksums.txt lists %s twice with different digests", name)
		}
		found = sum
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("%w for %s in checksums.txt", ErrNoChecksum, name)
	}
	return found, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
