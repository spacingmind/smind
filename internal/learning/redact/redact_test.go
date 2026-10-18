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
		"aws key":          "key is " + "AKIA" + "ABCDEFGHIJKLMNOP" + " ok",
		"pem full":         "-----BEGIN RSA PRIVATE KEY-----\nMIIEow" + "ABCDEF" + "\n-----END RSA PRIVATE KEY-----",
		"pem lone header":  "-----BEGIN PRIVATE KEY-----\nabc12345678901234567890123456789012345678901234567890123456789012345678901234567890",
		"github token":     "tok " + "ghp_" + strings.Repeat("Ab1", 12),
		"github fine":      "tok github_pat_" + strings.Repeat("aB1_", 7),
		"slack token":      "tok " + "xoxb-" + "qW9eR5tY2u" + "iO3pA7sD6f",
		"jwt":              "jwt " + "eyJhbGciOiJIUzI1NiIs" + "InR5cCI6IkpXVCJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIn0." + strings.Repeat("S1c", 8),
		"bearer":           "creds bearer " + strings.Repeat("aB1.", 5) + "xyz1",
		"key=value":        `DB_PASSWORD=hunter2secret`,
		"api_key quoted":   `config api_key: "supersecretvalue123"`,
		"email":            "contact alice.example+tag@example.com now",
		"card luhn":        "card 4111 1111 1111 1111 charged",
		"card nonluhn":     "card 4111 1111 1111 1112 charged",
		"git sha":          "commit " + strings.Repeat("ab1f", 10),
		"sha256":           strings.Repeat("c0f", 21) + "e",
		"base64ish":        "qW9eR5tY2uiO3pA7sD6fGh4jK",
		"camelcase":        "TestWindowCapsRecordInputResultAndTotal",
		"path":             "internal/learning/redact/redact_test.go",
		"home users":       "/Users/alice/src/x",
		"home explicit":    "/data/home/bob/src/main.go",
		"home windows":     `C:\Users\Carol\proj\main.go`,
		"plain":            "nothing interesting here",
		"json apikey":      `{"apiKey": "` + "ZGHFS43f" + `ks9dmas3"}`,
		"json escaped":     `{\"password\": \"` + "hunter2" + `secret42\"}`,
		"json env":         `{"env":{"OPENAI_API_KEY":"` + "sk-test" + `1234abcd"}}`,
		"single quoted":    "FOO_SECRET='" + "hunter2" + "secret'",
		"auth token":       "Authorization: Token " + "abcdef0123" + "456789abcdef",
		"auth basic":       "Authorization: Basic " + "dXNlcjpwYXNz" + "d29yZDEyMzQ1Njc4",
		"auth bearer":      "Authorization: Bearer " + "abcdef0123" + "456789abcdef",
		"query":            "https://x/y?key=" + "abcdefgh" + "1234&sig=zzzzzzzz",
		"card tail digits": "paid 4111 1111 1111 1111 123 ok",
		"card expiry":      "4111 1111 1111 1111 12/26",
		"card year":        "4111111111111111 12 2026",
		"card prefixed":    "order 99 4111 1111 1111 1111",
		"timestamp":        "t=1760812345678 id=1000000000000000001 2026-10-18 12:30:45",
		"hmac hex":         "HMAC f09e2e8c662248b483b7ffc050fec94dbca3a0aac36098b2cc2bd818319478da",
		"commit hex":       "commit 6bd0c621de49f145fda9988c79fc35526f7eaed4",
		"git log hex":      "git log: f09e2e8c662248b483b7ffc050fec94dbca3a0aac36098b2cc2bd818319478da",
		"bare hex":         "6725a2a7b860dcd6c8a1f8b46287cced9041dff0",
		"path deep":        "web/packages/ui/src/lib/v2/foo and see web/packages/ui/src/components/Button2/index",
		"pgp block":        "-----BEGIN PGP PRIVATE KEY BLOCK-----\nlQOY" + "ABCDEF" + "\n-----END PGP PRIVATE KEY BLOCK-----",
		"url creds":        "postgres://admin:s3cret@localhost:5432/app redis://:s3cretpw@cache:6379",
		"curl -u":          "curl -u admin:s3cr3tpassw0rd http://x",
		"prefixed":         "k " + "npm_" + "abcdefghijklmnopqrst" + " " + "hf_" + "abcdefghijklmnopqrstuvwx",
		"home boundary":    "/home/alice/x and D:\\Users\\bob\\proj and \\\\?\\C:\\Users\\bob\\x /root/y",
		"counters":         "max_tokens: 4096 and credentials: none",
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
	check("pgp block", func(s string) bool { return r.String(s) == Placeholder })
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

func TestRedact_CardWindows(t *testing.T) {
	r := New(nil, nil)
	for in, want := range map[string]string{
		"4111 1111 1111 1111 123":      Placeholder + " 123",
		"4111 1111 1111 1111 12/26":    Placeholder + " 12/26",
		"4111111111111111 12 2026":     Placeholder + " 12 2026",
		"order 99 4111 1111 1111 1111": "order 99 " + Placeholder,
		"4111 1111 1111 1112":          "4111 1111 1111 1112",
		"1760812345678":                "1760812345678",
		"1000000000000000001":          "1000000000000000001",
		"2026-10-18 12:30:45":          "2026-10-18 12:30:45",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_HexSecretsVsGitSha(t *testing.T) {
	r := New(nil, nil)
	const h64 = "f09e2e8c662248b483b7ffc050fec94dbca3a0aac36098b2cc2bd818319478da"
	const h40 = "6bd0c621de49f145fda9988c79fc35526f7eaed4"
	for in, want := range map[string]string{
		"HMAC " + h64:                    "HMAC " + Placeholder,
		"commit " + h40:                  "commit " + h40,
		"git log: " + h64:                "git log: " + h64,
		h40:                              Placeholder,
		"web/packages/ui/src/lib/v2/foo": "web/packages/ui/src/lib/v2/foo",
		"see web/packages/ui/src/components/Button2/index": "see web/packages/ui/src/components/Button2/index",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	for _, h := range hex24 {
		if e := shannon(h); e < 3.0 {
			t.Fatalf("fixture %s has entropy %.2f < 3.0", h, e)
		}
		if got := r.String("id " + h); got != "id "+Placeholder {
			t.Errorf("%s: got %q, want redacted", h, got)
		}
	}
}

var hex24 = []string{
	"149d439536b3216fdaeeb975",
	"729fae923d5a4fd12aabfe22",
	"8f219e9cb0eb53f16947ccf2",
	"5ec84d8dbc74254770f58904",
	"dba41ecccc3fc1626e53a130",
	"43b026c48bbf33feff9243a8",
	"f506b40928b5b7a767c76fb0",
	"08f86bebb2737f6a6f0fb23c",
	"6f5da2cec255404e4fb44003",
	"4d6608697a8d41bed440e504",
	"54f31af3176813e02ea68ef7",
	"86e4d3cea27d26934b484e73",
	"cf575dcad6ba2b0aee0ca923",
	"732881584d8c4fa2815d2802",
	"827283e0ad84173581569969",
	"e58b081006f7e3dfc967a64c",
	"b14028d512c9791e558e08ba",
	"a7196b50ac2f86702824c1c0",
	"99724caf4941d4072014b3ce",
	"624a8940f1f836f99eee3692",
}

func TestRedact_UrlCredentials(t *testing.T) {
	r := New(nil, nil)
	for in, want := range map[string]string{
		"postgres://admin:s3cret@localhost:5432/app":  "postgres://admin:[REDACTED]@localhost:5432/app",
		"redis://:s3cretpw@cache:6379":                "redis://:[REDACTED]@cache:6379",
		"DATABASE_URL=postgres://u:pw1234@db/x":       "DATABASE_URL=postgres://u:[REDACTED]@db/x",
		"curl -u admin:s3cr3tpassw0rd http://x":       "curl -u [REDACTED] http://x",
		"curl --user admin:s3cr3tpassw0rd http://x/y": "curl --user [REDACTED] http://x/y",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestRedact_PrefixedKeys(t *testing.T) {
	r := New(nil, nil)
	for _, in := range []string{
		"sk-ant-api03-" + "AbCd_eFgh-IjKl" + "MnOp_QrSt-UvWxYz01",
		"sk-proj-" + "AbCdEfGhIjKlMnOpQrSt" + "UvWx",
		"AIza" + "SyA1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q",
		"npm_" + "AbCdEfGhIjKlMnOpQrSt",
		"hf_" + "abcdefghijklmnopqrstuvwx",
		"rk_live_" + "abcdefghijklmnopqrstuvwx",
		"glpat-" + "AbCdEfGhIjKlMnOpQrSt",
		"xapp-" + "1-A0123456789-abcdef",
	} {
		if got := r.String("tok " + in + " end"); got != "tok "+Placeholder+" end" {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestRedact_HomePrefixBoundary(t *testing.T) {
	for _, tc := range []struct {
		homes    []string
		in, want string
	}{
		{[]string{"/home/al"}, "/home/alice/x", "~/x"},
		{[]string{"/home/al"}, "cd /home/al/x", "cd ~/x"},
		{[]string{"/home/al"}, "/home/al", "~"},
		{[]string{"/Users/bo"}, "/Users/bob/x", "~/x"},
		{nil, `D:\Users\bob\proj`, `~\proj`},
		{nil, `\\?\C:\Users\bob\proj`, `~\proj`},
		{nil, "/root/proj and /root", "~/proj and ~"},
		{nil, "/var/root/x /rooted/y", "/var/root/x /rooted/y"},
	} {
		if got := New(nil, tc.homes).String(tc.in); got != tc.want {
			t.Errorf("%v %q: got %q, want %q", tc.homes, tc.in, got, tc.want)
		}
	}
}

func TestRedact_EncodedExactSecrets(t *testing.T) {
	const secret = `pa"ss word/123x`
	r := New([]string{secret}, nil)
	for name, form := range map[string]string{
		"raw":   secret,
		"json":  `pa\"ss word/123x`,
		"query": `pa%22ss+word%2F123x`,
		"path":  `pa%22ss%20word%2F123x`,
	} {
		if got := r.String("val " + form + " end"); got != "val "+Placeholder+" end" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}
