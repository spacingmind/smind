package crash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// redactVector is one golden case shared with the desktop shell's Rust crate
// (desktop/crash-report reads the same file): both implementations must
// produce `want` for `input`.
type redactVector struct {
	Name      string `json:"name"`
	Input     string `json:"input"`
	Home      string `json:"home"`
	SmindHome string `json:"smindHome"`
	Want      string `json:"want"`
}

const vectorsFile = "testdata/redact_vectors.json"

// vectorInputs are the cases; `want` is produced by Redact and committed, so
// a change of the Go rules shows up as a diff of the file (regenerate with
// CRASH_UPDATE_VECTORS=1 go test ./internal/crash -run RedactionMatchesGoldenVectors)
// and the Rust side fails until it is mirrored.
func vectorInputs() []redactVector {
	const home, smind = "/home/tester", "/home/tester/.smind"
	v := func(name, in string) redactVector {
		return redactVector{Name: name, Input: in, Home: home, SmindHome: smind}
	}
	return []redactVector{
		v("home dir", "open /home/tester/code/x.go: no such file"),
		v("smind home before home", "read /home/tester/.smind/token failed"),
		v("home with backslashes", `C:\home\tester\x`),
		v("home boundary sibling", "/home/testerson/x.go and /home/tester/y.go and /home/tester2/z.go"),
		v("home trailing dot", "see /home/tester."),
		v("other unix user", "/Users/bob/go/pkg/mod/x.go:12 +0x1c"),
		v("other linux user", "/home/alice/work/y.go"),
		v("root user", "/root/.cache/z"),
		v("rootfs not root", "/rootfs/etc"),
		v("windows user", `C:\Users\carol\AppData\x.go`),
		v("windows user slash", "C:/Users/carol/AppData/x.go"),
		v("windows user with space", `C:\Users\Alice Smith\go\pkg\x.go:12 +0x1`),
		v("mac user with space", "/Users/Alice Smith/go/x.go:12 +0x1"),
		v("linux user three words", "/home/Jo Ann Marie/code/x.go"),
		v("user dir then colon", "open /Users/bob: permission denied"),
		v("unicode user", "/Users/Åsa Öberg/x.go"),
		v("bearer", "Authorization failed for Bearer abcDEF123456.token_value"),
		v("basic", "got Basic dXNlcjpwYXNzd29yZA== from proxy"),
		v("sk key", "key sk-abcdefghij1234567890"),
		v("github token", "token ghp_0123456789abcdef0123"),
		v("github pat", "github_pat_11ABCDEFG0123456789_abcdefghij"),
		v("aws key", "AKIAABCDEFGHIJKLMNOP in env"),
		v("jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"),
		v("slack", "token xoxb-1234567890-abcdefGHIJ"),
		v("google api key", "key AIzaSyA1234567890abcdefghijklmnopqrstuv"),
		v("gitlab", "glpat-abcdefghij1234567890"),
		v("npm", "npm_abcdefghijklmnopqrstuvwxyz0123456789"),
		v("google oauth", "ya29.a0AfH6SMBabcdefghijklmnop"),
		v("stripe", "sk_live_abcdefghijklmnop1234"),
		v("pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAsecretbody\n-----END RSA PRIVATE KEY-----"),
		v("postgres url", "dial postgres://admin:hunter2@db.internal:5432/x"),
		v("https url userinfo", "GET https://user:pa55@example.com/x"),
		v("key equals value", "password=hunter2 and secret: s3cret"),
		v("json authorization", `{"authorization":"Bearer abcDEF123456"}`),
		v("quoted password with spaces", `password: "x y z-secret"`),
		v("single quoted api key", `api_key='my secret value'`),
		v("env style", "DB_PASS=hunter2hunter2 FOO_TOKEN=abc123def"),
		v("auth token scheme", "Authorization: Token abc123"),
		v("auth apikey scheme", "authorization: ApiKey k-123-zzz"),
		v("auth digest", "Proxy-Authorization: Digest username=u, response=xyz789"),
		v("cookie", "Cookie: sid=abc123; theme=dark"),
		v("set-cookie", "Set-Cookie: sid=abc123; Path=/"),
		v("bare token scheme", "retry with token abcdefghijklmnop"),
		v("x-api-key", "x-api-key: abc123def"),
		v("hex run", "digest deadbeefdeadbeefdeadbeefdeadbeef"),
		v("short hex untouched", "addr 0x14000123456 and 0xdeadbeef"),
		v("high entropy run", "id Abcdefghij0123456789ABCDEFGHijkl rest"),
		v("long lowercase identifier untouched", "pkg.abcdefghijklmnopqrstuvwxyzabcdefghij"),
		v("clean stack untouched", "goroutine 7 [running]:\ngithub.com/spacingmind/smind/internal/wsapi.(*conn).dispatch.func1()\n\t/usr/local/go/src/runtime/panic.go:787 +0x130\n"),
		v("path in quoted value", `token="/home/tester/.smind/x y"`),
		v("nothing to do", "index out of range [5] with length 3"),
		{Name: "empty home", Input: "/home/tester/x and /Users/bob/y", Home: "", SmindHome: ""},
		{Name: "root home ignored", Input: "/etc/passwd /home/tester", Home: "/", SmindHome: ""},
		{Name: "trailing slash home", Input: "/home/tester/x", Home: "/home/tester/", SmindHome: ""},
	}
}

// crash-go-redaction-matches-golden-vectors: the committed vectors equal what
// Redact produces today.
func TestCrashGoRedactionMatchesGoldenVectors(t *testing.T) {
	cases := vectorInputs()
	for i := range cases {
		cases[i].Want = Redact(cases[i].Input, cases[i].Home, cases[i].SmindHome)
	}
	if os.Getenv("CRASH_UPDATE_VECTORS") == "1" {
		data, err := json.MarshalIndent(cases, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsFile, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(filepath.FromSlash(vectorsFile))
	if err != nil {
		t.Fatalf("%v (regenerate with CRASH_UPDATE_VECTORS=1)", err)
	}
	var committed []redactVector
	if err := json.Unmarshal(data, &committed); err != nil {
		t.Fatal(err)
	}
	if len(committed) != len(cases) {
		t.Fatalf("the vector file has %d cases, the generator %d (regenerate with CRASH_UPDATE_VECTORS=1)", len(committed), len(cases))
	}
	for i, c := range cases {
		if committed[i] != c {
			t.Errorf("vector %q differs from what Redact produces now:\n committed want %q\n current      want %q", c.Name, committed[i].Want, c.Want)
		}
	}
}
