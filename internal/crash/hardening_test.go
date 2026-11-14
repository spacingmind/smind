package crash

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// crash-redact-path-boundary: the home only matches whole path components.
func TestCrashRedactPathBoundary(t *testing.T) {
	got := Redact("/Users/alicebob/x.go and /Users/alice/y.go and /Users/alice2/z.go", "/Users/alice", "")
	for _, leak := range []string{"alice", "bob", "alice2"} {
		if strings.Contains(got, leak) {
			t.Fatalf("%q leaked in %q", leak, got)
		}
	}
	if !strings.Contains(got, "~/y.go") {
		t.Fatalf("the exact home was not replaced by ~: %q", got)
	}
}

// crash-redact-more-secret-shapes: vendor tokens, quoted values, JSON keys,
// other URL schemes, Basic auth, PEM bodies, passphrases and cookies.
func TestCrashRedactMoreSecretShapes(t *testing.T) {
	cases := []struct{ name, in, secret string }{
		{"slack", "token xoxb-1234567890-abcdefGHIJ", "xoxb-1234567890"},
		{"google api key", "key AIzaSyA1234567890abcdefghijklmnopqrstuv", "AIzaSyA1234567890"},
		{"gitlab", "glpat-abcdefghij1234567890", "glpat-abcdefghij"},
		{"npm", "npm_abcdefghijklmnopqrstuvwxyz0123456789", "npm_abcdefghij"},
		{"google oauth", "ya29.a0AfH6SMBabcdefghijklmnop", "ya29.a0AfH6SMB"},
		{"stripe", "sk_live_abcdefghijklmnop1234", "sk_live_abcdefghij"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAsecretbody\n-----END RSA PRIVATE KEY-----", "secretbody"},
		{"postgres url", "dial postgres://admin:hunter2@db.internal:5432/x", "hunter2"},
		{"ssh url", "ssh://deploy:s3cret@host/repo", "s3cret"},
		{"json authorization", `{"authorization":"Bearer abcDEF123456"}`, "abcDEF123456"},
		{"quoted password with spaces", `password: "x y z-secret"`, "z-secret"},
		{"single quoted", `api_key='my secret value'`, "secret value"},
		{"basic auth", "Authorization: Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA"},
		{"db pass env", "DB_PASS=hunter2hunter2", "hunter2hunter2"},
		{"cookie", "cookie: sessionid=abcdef0123456789", "abcdef0123456789"},
		{"passphrase", "passphrase=correct-horse-battery", "correct-horse-battery"},
		{"32 char token", "id Abcdefghij0123456789ABCDEFGHijkl rest", "Abcdefghij0123456789ABCDEFGHijkl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.in, "/home/tester", "/home/tester/.smind"); strings.Contains(got, c.secret) {
				t.Fatalf("secret %q survived: %q", c.secret, got)
			}
		})
	}
}

// crash-redact-idempotent-and-keeps-frames: the stored text is stable under
// Redact, and ordinary stack frames are not mangled.
func TestCrashRedactIdempotentAndKeepsFrames(t *testing.T) {
	frames := "goroutine 7 [running]:\n" +
		"github.com/spacingmind/smind/internal/wsapi.(*conn).dispatch.func1()\n" +
		"\t~/code/smind/internal/wsapi/conn.go:219 +0x1c4\n" +
		"github.com/spacingmind/smind/internal/runs.(*Registry).drive(0x14000123456, 0x1400098abcd)\n" +
		"\t/usr/local/go/src/runtime/panic.go:787 +0x130\n"
	if got := Redact(frames, "/home/tester", ""); got != frames {
		t.Fatalf("a clean stack was changed:\n%s", got)
	}
	for _, in := range []string{
		`password="a b" token=xoxb-1234567890-abcdef /Users/bob/x`,
		"postgres://u:p@h and Bearer abc.def and /home/tester/.smind/token",
	} {
		once := Redact(in, "/home/tester", "/home/tester/.smind")
		if twice := Redact(once, "/home/tester", "/home/tester/.smind"); twice != once {
			t.Fatalf("not stable: %q -> %q", once, twice)
		}
	}
}

// crash-panic-type-redacted: %T of an anonymous type can carry text (struct tags).
func TestCrashPanicTypeRedacted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	r := New(testConfig(dir))
	v := struct {
		Name string `json:"token=abc123secret"`
	}{}
	path := r.Record("type.site", v, []byte("x.Fn()\n"))
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "abc123secret") {
		t.Fatalf("the panic type leaked: %s", data)
	}
}

func panicsHere(f func()) {
	defer Recover("shared.site")
	f()
}

func firstBug()  { panicsHere(func() { var m map[string]int; m["a"] = 1 }) }
func secondBug() { panicsHere(func() { var s []int; _ = s[3] }) }

// crash-dedupe-distinct-sites: the dedupe key is the panicking frame, not the
// panic value's type: two different bugs at one site are both recorded.
func TestCrashDedupeDistinctFrames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	setDefault(t, testConfig(dir))
	firstBug()
	secondBug()
	if files := crashFiles(t, dir); len(files) != 2 {
		t.Fatalf("reports = %d, want 2 (two different panicking frames)", len(files))
	}
	secondBug() // the same bug again inside the window is suppressed
	if files := crashFiles(t, dir); len(files) != 2 {
		t.Fatalf("reports = %d after a repeat, want still 2", len(files))
	}
	_ = debug.Stack // keep the import honest if the helpers change
}

// crash-same-millisecond-no-overwrite: two different panics at one site in one
// millisecond are two files.
func TestCrashSameMillisecondNoOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	cfg := testConfig(dir)
	fixed := time.Date(2026, 11, 15, 1, 2, 3, 4_000_000, time.UTC)
	cfg.Now = func() time.Time { return fixed }
	r := New(cfg)
	a := r.Record("same.site", "x", []byte("a.One()\n"))
	b := r.Record("same.site", "x", []byte("b.Two()\n"))
	if a == "" || b == "" || a == b {
		t.Fatalf("paths %q and %q: want two distinct files", a, b)
	}
	if files := crashFiles(t, dir); len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
}

// crash-tmp-files-of-others-kept: a fresh .tmp-crash file (another process in
// flight) survives rotation; an old one is removed.
func TestCrashTmpFilesOfOthersKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, ".tmp-crash-9999-1")
	old := filepath.Join(dir, ".tmp-crash-9999-2")
	for _, p := range []string{fresh, old} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	r := New(testConfig(dir))
	if r.Record("tmp.site", "x", []byte("a.One()\n")) == "" {
		t.Fatal("no report written")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a fresh temp file of another writer was removed: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("an old leftover temp file was kept (err = %v)", err)
	}
}

// crash-redact-user-dir-with-spaces: a Windows or macOS user name with spaces
// is removed whole, not left half-visible.
func TestCrashRedactUserDirWithSpaces(t *testing.T) {
	cases := []struct {
		in   string
		gone []string
		keep string
	}{
		{`C:\Users\Alice Smith\go\pkg\x.go:12 +0x1`, []string{"Alice", "Smith"}, `\go\pkg\x.go:12 +0x1`},
		{`C:/Users/Alice Smith/go/x.go`, []string{"Alice", "Smith"}, `/go/x.go`},
		{"/Users/Alice Smith/go/x.go:12 +0x1", []string{"Alice", "Smith"}, "/go/x.go:12 +0x1"},
		{"/home/Jo Ann Marie/code/x.go", []string{"Jo", "Ann", "Marie"}, "/code/x.go"},
		{"open /Users/bob: permission denied", []string{"bob"}, ": permission denied"},
		{"/Users/Åsa Öberg/x.go", []string{"Åsa", "Öberg"}, "/x.go"},
	}
	for _, c := range cases {
		got := Redact(c.in, "/nobody", "")
		for _, g := range c.gone {
			if strings.Contains(got, g) {
				t.Errorf("Redact(%q) = %q: %q survived", c.in, got, g)
			}
		}
		if !strings.Contains(got, c.keep) {
			t.Errorf("Redact(%q) = %q: lost the rest of the path %q", c.in, got, c.keep)
		}
	}
}

// crash-redact-auth-scheme-credentials: an Authorization / Cookie value is
// removed whole, whatever the scheme word is.
func TestCrashRedactAuthSchemeCredentials(t *testing.T) {
	for _, c := range []struct{ in, secret string }{
		{"Authorization: Token abc123", "abc123"},
		{"authorization: ApiKey k-123-zzz", "k-123-zzz"},
		{`"Authorization": "Token abc123"`, "abc123"},
		{"Proxy-Authorization: Digest username=u, response=xyz789", "xyz789"},
		{"Authorization: Negotiate YIIabc123", "YIIabc123"},
		{"Cookie: sid=abc123; theme=dark", "abc123"},
		{"Set-Cookie: sid=abc123; Path=/", "abc123"},
		{"retry with token abcdefghijklmnop", "abcdefghijklmnop"},
		{"x-api-key: abc123def", "abc123def"},
	} {
		if got := Redact(c.in, "/nobody", ""); strings.Contains(got, c.secret) {
			t.Errorf("Redact(%q) = %q: %q survived", c.in, got, c.secret)
		}
	}
}
