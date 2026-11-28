package failure

import (
	"testing"
	"time"
)

func Test_retryinfo_string_and_object_forms(t *testing.T) {
	cases := []struct {
		name, body string
		want       time.Duration
	}{
		{"fraction", `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"3.5s"}]}}`, 3500 * time.Millisecond},
		{"seconds", `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"120s"}]}}`, 120 * time.Second},
		{"object", `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":{"seconds":3,"nanos":500000000}}]}}`, 3500 * time.Millisecond},
		{"object string secs", `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":{"seconds":"7"}}]}}`, 7 * time.Second},
	}
	for _, c := range cases {
		got, ok := ParseRetryInfo([]byte(c.body))
		if !ok || got != c.want {
			t.Errorf("%s: got %v,%v want %v", c.name, got, ok, c.want)
		}
	}
}

func Test_retryinfo_rejects_garbage_negative_zero(t *testing.T) {
	d := func(v string) string {
		return `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":` + v + `}]}}`
	}
	bodies := []string{
		"not json", d(`"-1s"`), d(`"0s"`), d(`"abc"`), d(`"NaNs"`), d(`{"seconds":2000000000}`),
		`{"error":{"message":"x"}}`,
		`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo"}]}}`,
	}
	for _, b := range bodies {
		if got, ok := ParseRetryInfo([]byte(b)); ok || got != 0 {
			t.Errorf("%q: got %v,%v want 0,false", b, got, ok)
		}
	}
}

func Test_retryinfo_found_among_other_details(t *testing.T) {
	body := `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[]},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"9s"}]}}`
	if got, ok := ParseRetryInfo([]byte(body)); !ok || got != 9*time.Second {
		t.Fatalf("nested: %v,%v", got, ok)
	}
	top := `{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"4s"}]}`
	if got, ok := ParseRetryInfo([]byte(top)); !ok || got != 4*time.Second {
		t.Fatalf("top-level: %v,%v", got, ok)
	}
}
