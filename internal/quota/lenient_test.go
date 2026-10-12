package quota

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLenientFloat(t *testing.T) {
	t.Parallel()

	decode := func(t *testing.T, data string) (*float64, error) {
		t.Helper()
		var f lenientFloat
		err := json.Unmarshal([]byte(data), &f)
		return f.ptr(), err
	}
	num := func(v float64) *float64 { return &v }

	tests := []struct {
		name    string
		data    string
		want    *float64
		wantErr bool
	}{
		{"number", `12.5`, num(12.5), false},
		{"numeric string", `"42"`, num(42), false},
		{"padded string", `" 12 "`, num(12), false},
		{"null", `null`, nil, false},
		{"non-numeric string", `"abc"`, nil, true},
		{"NaN string", `"NaN"`, nil, true},
		{"Inf string", `"Inf"`, nil, true},
		{"object", `{"a":1}`, nil, true},
		{"array", `[1]`, nil, true},
		{"bool", `true`, nil, true},
		{"large exponent", `1e999`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decode(t, tt.data)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Unmarshal(%s) error = %v, wantErr %v", tt.data, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.want == nil || got == nil {
				if tt.want != got {
					t.Errorf("value = %v, want %v", got, tt.want)
				}
				return
			}
			if *got != *tt.want {
				t.Errorf("value = %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		v    string
		want time.Duration
	}{
		{"seconds", "120", 2 * time.Minute},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"empty", "", 0},
		{"junk", "soon", 0},
		{"absurd", "99999999999999", 0},
		{"http-date future", now.Add(90 * time.Second).In(time.FixedZone("GMT", 0)).Format(time.RFC1123), 90 * time.Second},
		{"http-date past", now.Add(-time.Hour).In(time.FixedZone("GMT", 0)).Format(time.RFC1123), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseRetryAfter(tt.v, now); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestLooksLikeHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{"html tag", "<html>", true},
		{"leading whitespace doctype", "  \n<!DOCTYPE", true},
		{"json object", "{}", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeHTML([]byte(tt.body)); got != tt.want {
				t.Errorf("looksLikeHTML(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestParseRFC3339(t *testing.T) {
	t.Parallel()

	valid := "2026-03-01T12:00:00Z"
	tests := []struct {
		name string
		in   *string
		want bool
	}{
		{"valid", &valid, true},
		{"nil", nil, false},
		{"empty", strptr(""), false},
		{"junk", strptr("yesterday"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseRFC3339(tt.in)
			if (got != nil) != tt.want {
				t.Errorf("parseRFC3339(%v) = %v, want non-nil: %v", tt.in, got, tt.want)
			}
		})
	}
}

func strptr(s string) *string { return &s }

func TestParseRFC3339RejectsUnmarshalableYears(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"0000-01-01T00:00:00+05:00", "9999-12-31T23:59:59-05:00"} {
		v := s
		if got := parseRFC3339(&v); got != nil {
			t.Errorf("parseRFC3339(%q) = %v, want nil", s, got)
		}
	}
	ok := "2026-10-12T05:00:00.5+00:00"
	if got := parseRFC3339(&ok); got == nil {
		t.Errorf("parseRFC3339(%q) = nil, want a time", ok)
	}
}

func TestParseRetryAfterNaNAndInf(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf"} {
		if got := parseRetryAfter(v, time.Unix(0, 0)); got != 0 {
			t.Errorf("parseRetryAfter(%q) = %v, want 0", v, got)
		}
	}
}

func TestCapText(t *testing.T) {
	t.Parallel()
	if got := capText("  hi  "); got != "hi" {
		t.Errorf("capText trim = %q", got)
	}
	long := strings.Repeat("é", 500)
	if got := capText(long); len([]rune(got)) != maxTextLen {
		t.Errorf("capText length = %d runes, want %d", len([]rune(got)), maxTextLen)
	}
}
