package browsercard

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"removes control chars", "a\x00\x01\x02b", 50, "ab"},
		{"collapses newline and tab runs", "hello \t \n world\n\tagain", 50, "hello world again"},
		{"trims surrounding whitespace", "  \t spaced \n ", 50, "spaced"},
		{"collapses tab between words", "left\tright", 50, "left right"},
		{"removes U+2028 and U+2029", "a\u2028b\u2029c", 50, "abc"},
		{"empty stays empty", "", 50, ""},
		{"at cap unchanged", "12345", 5, "12345"},
		{"over cap gets ellipsis", "123456", 5, "1234…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanText(tt.in, tt.max); got != tt.want {
				t.Errorf("cleanText(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestCleanTextRuneCapMultibyte(t *testing.T) {
	got := cleanText(strings.Repeat("é", 300), 10)
	want := strings.Repeat("é", 9) + "…"
	if got != want {
		t.Errorf("cleanText multibyte = %q, want %q", got, want)
	}
	if n := utf8.RuneCountInString(got); n != 10 {
		t.Errorf("cleanText multibyte is %d runes, want 10", n)
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"userinfo dropped", "https://user:secret@example.com/x", "https://example.com/x"},
		{"query dropped", "http://example.com/p?a=b&c=d", "http://example.com/p"},
		{"fragment dropped", "http://example.com/p#section", "http://example.com/p"},
		{"explicit port kept", "http://example.com:8080/p", "http://example.com:8080/p"},
		{"ipv6 literal kept in brackets", "http://[2001:db8::1]:9000/x", "http://[2001:db8::1]:9000/x"},
		{"https ok", "https://example.com/a/b", "https://example.com/a/b"},
		{"ftp rejected", "ftp://example.com/file", "(not a web address)"},
		{"javascript rejected", "javascript:alert(1)", "(not a web address)"},
		{"data rejected", "data:text/html,hello", "(not a web address)"},
		{"file rejected", "file:///etc/passwd", "(not a web address)"},
		{"plain text rejected", "not a url", "(not a web address)"},
		{"empty rejected", "", "(not a web address)"},
		{"host with trailing dot kept", "http://example.com./p", "http://example.com./p"},
		{
			"path of 300 chars cut to 120 runes incl. the leading slash and ellipsis",
			"http://example.com/" + strings.Repeat("a", 300),
			"http://example.com/" + strings.Repeat("a", 118) + "…",
		},
		{"empty path becomes slash", "http://example.com", "http://example.com/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURL(tt.raw); got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRedactURLPathCapIsRunes(t *testing.T) {
	got := redactURL("http://example.com/" + strings.Repeat("é", 300))
	path := strings.TrimPrefix(got, "http://example.com")
	if n := utf8.RuneCountInString(path); n != 120 {
		t.Errorf("capped path is %d runes, want 120 incl. leading slash and ellipsis (path %q)", n, path)
	}
}
