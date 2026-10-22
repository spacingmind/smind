package attach

import (
	"reflect"
	"strings"
	"testing"
)

func TestSafeName_Table(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"clipboard.png", 120, "clipboard.png"},
		{"../../etc/passwd", 120, "passwd"},
		{`C:\Users\x\a.txt`, 120, "a.txt"},
		{"a/b\\c.txt", 120, "c.txt"},
		{"bad\x00na\x07me.txt", 120, "badname.txt"},
		{"..hidden", 120, "hidden"},
		{"  .. x", 120, "x"},
		{"", 120, "file"},
		{"/", 120, "file"},
		{"...", 120, "file"},
		{"héllo.txt", 2, "h"},
		{"héllo.txt", 3, "hé"},
		{"日本語.png", 4, "日"},
		{strings.Repeat("a", 200), 120, strings.Repeat("a", 120)},
	}
	for _, c := range cases {
		if got := SafeName(c.in, c.max); got != c.want {
			t.Errorf("SafeName(%q,%d)=%q want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestSet_EncodeDecodeRoundTrip(t *testing.T) {
	if got := EncodeSet(Set{ID: "x"}); got != "" {
		t.Fatalf("empty set encoded to %q", got)
	}
	if s, err := DecodeSet(""); err != nil || !reflect.DeepEqual(s, Set{}) {
		t.Fatalf("DecodeSet(\"\") = %+v, %v", s, err)
	}
	in := Set{ID: "ab", Items: []Item{
		{ID: "1", Kind: KindImage, Name: "a.png", MimeType: "image/png", Size: 3, Width: 1, Height: 2, File: "01-a.png"},
		{ID: "2", Kind: KindFileRef, Path: "x/y.go", IsDir: false},
		{ID: "3", Kind: KindGithubPR, Number: 7, Repo: "o/r", Title: "t", URL: "u", State: "open", BaseRef: "b", HeadRef: "h", File: "03-body.md"},
	}}
	out, err := DecodeSet(EncodeSet(in))
	if err != nil || !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip: %+v, %v", out, err)
	}
	pub := in.Public()
	for _, it := range pub {
		if it.File != "" {
			t.Fatalf("Public leaked File: %+v", it)
		}
	}
	if in.Items[0].File == "" {
		t.Fatal("Public mutated the original")
	}
	if _, err := DecodeSet("{nope"); err == nil {
		t.Fatal("want decode error")
	}
}

func TestError_Format(t *testing.T) {
	e := &Error{Code: CodeTooLarge, Message: "too big", Index: 2}
	if e.Error() != "attachment 3: too big" || e.RPCCode() != CodeTooLarge {
		t.Fatalf("%v %v", e.Error(), e.RPCCode())
	}
	if (&Error{Message: "m", Index: -1}).Error() != "m" {
		t.Fatal("index -1")
	}
}
