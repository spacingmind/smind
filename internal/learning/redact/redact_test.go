package redact

import (
	"strings"
	"testing"
)

// corpus is every input the tests feed through String; idempotence is
// checked over all of it. Fake-but-shaped fixtures are built by
// concatenation so no literal looks like a real secret.
func corpus(r *Redactor) map[string]string {
	return map[string]string{
		"aws key":         "key is " + "AKIA" + "ABCDEFGHIJKLMNOP" + " ok",
		"pem full":        "-----BEGIN RSA PRIVATE KEY-----\nMIIEow" + "ABCDEF" + "\n-----END RSA PRIVATE KEY-----",
		"pem lone header": "-----BEGIN PRIVATE KEY-----\nabc12345678901234567890123456789012345678901234567890123456789012345678901234567890",
		"github token":    "tok " + "ghp_" + strings.Repeat("Ab1", 12),
		"github fine":     "tok github_pat_" + strings.Repeat("aB1_", 7),
		"slack token":     "tok " + "xoxb-" + "qW9eR5tY2u" + "iO3pA7sD6f",
		"jwt":             "jwt " + "eyJhbGciOiJIUzI1NiIs" + "InR5cCI6IkpXVCJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIn0." + strings.Repeat("S1c", 8),
		"bearer":          "creds bearer " + strings.Repeat("aB1.", 5) + "xyz1",
		"key=value":       `DB_PASSWORD=hunter2secret`,
		"api_key quoted":  `config api_key: "supersecretvalue123"`,
		"email":           "contact alice.example+tag@example.com now",
		"card luhn":       "card 4111 1111 1111 1111 charged",
		"card nonluhn":    "card 4111 1111 1111 1112 charged",
		"git sha":         "commit " + strings.Repeat("ab1f", 10),
		"sha256":          strings.Repeat("c0f", 21) + "e",
		"base64ish":       "qW9eR5tY2uiO3pA7sD6fGh4jK",
		"camelcase":       "TestWindowCapsRecordInputResultAndTotal",
		"path":            "internal/learning/redact/redact_test.go",
		"home users":      "/Users/alice/src/x",
		"home explicit":   "/data/home/bob/src/main.go",
		"home windows":    `C:\Users\Carol\proj\main.go`,
		"plain":           "nothing interesting here",
		"json apikey":     `{"apiKey": "` + "ZGHFS43f" + `ks9dmas3"}`,
		"json escaped":    `{\"password\": \"` + "hunter2" + `secret42\"}`,
		"json env":        `{"env":{"OPENAI_API_KEY":"` + "sk-test" + `1234abcd"}}`,
		"single quoted":   "FOO_SECRET='" + "hunter2" + "secret'",
		"auth token":      "Authorization: Token " + "abcdef0123" + "456789abcdef",
		"auth basic":      "Authorization: Basic " + "dXNlcjpwYXNz" + "d29yZDEyMzQ1Njc4",
		"auth bearer":     "Authorization: Bearer " + "abcdef0123" + "456789abcdef",
		"query":           "https://x/y?key=" + "abcdefgh" + "1234&sig=zzzzzzzz",
		"counters":        "max_tokens: 4096 and credentials: none",
	}
}

func TestRedact_ExactMcpSecretsAndRunEnv(t *testing.T) {
	short := "abc123" // 7 bytes: ignored
	long := "supersecretvalue123456"
	longer := "supersecretvalue123456" + "extra"
	r := New([]string{short, long, longer}, nil)
	got := r.String("env has " + short + " and " + longer + " inside")
	want := "env has " + short + " and " + Placeholder + " inside"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// The shorter long secret is contained in longer; the longest wins and
	// the whole span becomes one placeholder (no nested re-replacement).
	got = r.String("value: " + long)
	if got != "value: "+Placeholder {
		t.Fatalf("contained secret: got %q, want single placeholder", got)
	}
}

func TestRedact_RuleCorpus(t *testing.T) {
	r := New(nil, nil)
	c := corpus(r)
	check := func(name string, f func(string) bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if !f(c[name]) {
				t.Fatalf("%q: condition failed (got %q)", c[name], r.String(c[name]))
			}
		})
	}
	check("aws key", func(s string) bool { return r.String(s) == "key is "+Placeholder+" ok" })
	check("pem full", func(s string) bool { return r.String(s) == Placeholder })
	check("pem lone header", func(s string) bool { return r.String(s) == Placeholder })
	check("github token", func(s string) bool { return r.String(s) == "tok "+Placeholder })
	check("github fine", func(s string) bool { return r.String(s) == "tok "+Placeholder })
	check("slack token", func(s string) bool { return r.String(s) == "tok "+Placeholder })
	check("jwt", func(s string) bool { return r.String(s) == "jwt "+Placeholder })
	check("bearer", func(s string) bool {
		return r.String(s) == "creds Bearer "+Placeholder
	})
	check("key=value", func(s string) bool { return r.String(s) == "DB_PASSWORD="+Placeholder })
	check("api_key quoted", func(s string) bool { return r.String(s) == `config api_key: "`+Placeholder+`"` })
	check("email", func(s string) bool { return r.String(s) == "contact "+Placeholder+" now" })
	check("card luhn", func(s string) bool { return r.String(s) == "card "+Placeholder+" charged" })
	check("card nonluhn", func(s string) bool { return !r.Changed(s) })
	check("git sha", func(s string) bool { return !r.Changed(s) })
	check("sha256", func(s string) bool { return !r.Changed(s) })
	check("base64ish", func(s string) bool { return r.String(s) == Placeholder })
	check("camelcase", func(s string) bool { return !r.Changed(s) })
	check("path", func(s string) bool { return !r.Changed(s) })
}

func TestRedact_HomePrefix(t *testing.T) {
	r := New(nil, []string{"/data/home/bob"})
	for in, want := range map[string]string{
		"/Users/alice/src/x":          "~/src/x",
		"/data/home/bob/src/main.go":  "~/src/main.go",
		`C:\Users\Carol\proj\main.go`: `~\proj\main.go`,
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_Idempotent(t *testing.T) {
	r := New([]string{"supersecretvalue123456"}, []string{"/data/home/bob"})
	for _, in := range corpus(r) {
		once := r.String(in)
		if again := r.String(once); again != once {
			t.Errorf("not idempotent: %q -> %q -> %q", in, once, again)
		}
	}
	if r.String(Placeholder) != Placeholder {
		t.Fatal("placeholder itself must not match a rule")
	}
}

func TestRedact_ChangedReportsDifference(t *testing.T) {
	r := New(nil, nil)
	if !r.Changed("mail me at alice@example.com please") {
		t.Fatal("Changed = false for a string String modifies")
	}
	if r.Changed("plain text only") {
		t.Fatal("Changed = true for an untouched string")
	}
}

func TestRedact_JSONQuotedKeys(t *testing.T) {
	r := New(nil, nil)
	for in, want := range map[string]string{
		`{"apiKey": "` + "ZGHFS43f" + `ks9dmas3"}`:               `{"apiKey": "[REDACTED]"}`,
		`{\"password\": \"` + "hunter2" + `secret42\"}`:          `{\"password\": \"[REDACTED]\"}`,
		`{"env":{"OPENAI_API_KEY":"` + "sk-test" + `1234abcd"}}`: `{"env":{"OPENAI_API_KEY":"[REDACTED]"}}`,
		"FOO_SECRET='" + "hunter2" + "secret'":                   "FOO_SECRET='[REDACTED]'",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_AuthHeaders(t *testing.T) {
	r := New(nil, nil)
	for in, want := range map[string]string{
		"Authorization: Token " + "abcdef0123" + "456789abcdef":       "Authorization: Token [REDACTED]",
		"Authorization: Basic " + "dXNlcjpwYXNz" + "d29yZDEyMzQ1Njc4": "Authorization: Basic [REDACTED]",
		"Authorization: Bearer " + "abcdef0123" + "456789abcdef":      "Authorization: Bearer [REDACTED]",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_KeyNamesAndQueryParams(t *testing.T) {
	r := New(nil, nil)
	for in, want := range map[string]string{
		"MY_KEY=" + "abcd1234efgh":                 "MY_KEY=[REDACTED]",
		"PRIVATE_KEY=" + "abcd1234efgh":            "PRIVATE_KEY=[REDACTED]",
		"SSH_KEY=" + "abcd1234efgh":                "SSH_KEY=[REDACTED]",
		"?key=" + "abcdefgh" + "1234&sig=zzzzzzzz": "?key=[REDACTED]&sig=[REDACTED]",
		"password: " + "hunter2" + "hunter2":       "password: [REDACTED]",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_NoOverRedactionOfCountersAndWords(t *testing.T) {
	r := New(nil, nil)
	for _, in := range []string{
		"max_tokens: 4096", "input_tokens: 1234", "tokenizer: gpt2",
		"credentials: none", "Authentication: failed with 401",
		"Authors: Alice and Bob", "author: Bob", "monkey: banana1234",
	} {
		if got := r.String(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}
