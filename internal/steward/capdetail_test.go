package steward

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapDetail_NeverSplitsRune(t *testing.T) {
	accented := strings.Repeat("é", 200) // 400 bytes, 2 bytes per rune
	got := capDetail(accented)
	if len(got) > 300 {
		t.Fatalf("capDetail returned %d bytes, want <= 300", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("capDetail split a rune: %q", got)
	}

	// The 300-byte cut lands one byte into a 3-byte rune: the trailing
	// partial rune must be backed off, not kept.
	mixed := strings.Repeat("é", 100) + strings.Repeat("あ", 67) // 401 bytes
	got = capDetail(mixed)
	if len(got) > 300 {
		t.Fatalf("capDetail(mixed) returned %d bytes, want <= 300", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("capDetail(mixed) split a rune: %q", got)
	}
}
