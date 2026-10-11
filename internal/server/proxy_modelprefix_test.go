package server

import (
	"strings"
	"testing"
)

func TestExtractModelPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		in    string
		want  string
		wantK bool
	}{
		{"model first", `{"model":"claude-opus-4"}`, "claude-opus-4", true},
		{"model after other keys", `{"stream":true,"model":"glm-4.6","x":1}`, "glm-4.6", true},
		{"model after nested object", `{"meta":{"a":{"b":1}},"model":"m"}`, "m", true},
		{"nested model not returned", `{"meta":{"model":"nested"},"other":2}`, "", false},
		{"truncated before model", `{"pad":"aaaa`, "", false},
		{"truncated at model value", `{"pad":"aaaa...","model":"clau`, "", false},
		{"no model", `{"stream":true}`, "", false},
		{"empty object", `{}`, "", false},
		{"non-string model", `{"model":42}`, "", false},
		{"null model", `{"model":null}`, "", false},
		{"not an object", `["model","x"]`, "", false},
		{"not JSON", "not json", "", false},
		{"empty input", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extractModelPrefix([]byte(tc.in))
			if ok != tc.wantK || got != tc.want {
				t.Fatalf("extractModelPrefix(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantK)
			}
		})
	}

	// A long pad before the model: model is beyond any early bail-out and
	// still found, mimicking an oversized-body prefix.
	long := `{"pad":"` + strings.Repeat("x", 1<<20) + `","model":"m"}`
	got, ok := extractModelPrefix([]byte(long))
	if !ok || got != "m" {
		t.Fatalf("long pad: got (%q, %v)", got, ok)
	}
}
