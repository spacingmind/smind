package crash

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
		v("user with parens", `C:\Users\jo(work)\x.go`),
		v("user with tilde short name", `C:\Users\ALICEJ~1\x.go`),
		v("user with dollar", "/Users/a$b/x.go"),
		v("unc profile not matched", `\\srv\home\alice\x`),
		v("extended length prefix", `\\?\C:\Users\carol\x.go`),
		v("crlf lines", "open /Users/bob/x.go\r\npassword=hunter2\r\nnext line"),
		v("tabs around key value", "token\t=\tabc123def456"),
		v("unicode space after key", "password=hunter\u2003two rest"),
		v("nbsp after bearer", "Bearer\u00a0abcdefghij123"),
		v("build machine cargo path", "/Users/builder/.cargo/registry/src/index.crates.io-1949cf8c6b5b557f/tokio-1.40.0/src/x.rs:12"),
		v("rustc std path", "/rustc/0123456789abcdef0123456789abcdef01234567/library/core/src/option.rs:1 +0x10"),
		v("very long clean line", strings.Repeat("a.b.c ", 400)),
		v("github oauth token", "token gho_0123456789abcdef0123"),
		v("github server token", "ghs_0123456789abcdef0123 leaked"),
		v("github user token", "x ghu_0123456789abcdef0123"),
		v("github refresh token", "ghr_0123456789abcdef0123."),
		v("aws session key", "ASIAABCDEFGHIJKLMNOP in env"),
		v("stripe restricted key", "rk_live_abcdefghijklmnop1234"),
		v("stripe test key", "sk_test_abcdefghijklmnop1234"),
		v("negotiate scheme bare", "send negotiate YIIabcdef123456789"),
		v("ntlm scheme bare", "send ntlm TlRMTVNTUAABAAAAB4IIAAAA"),
		v("apikey scheme bare", "use apikey abcdefghijklmnop now"),
		v("api-key scheme bare", "use api-key abcdefghijklmnop now"),
		v("digest scheme bare", "use digest abcdefghijklmnop now"),
		v("authorization negotiate line", "Authorization: Negotiate YIIabcdef123456789"),
		v("authorization ntlm line", "authorization: NTLM TlRMTVNTUAABAAAAB4IIAAAA"),
		v("three pass convergence", "token='a b'sk-abcdefghij1234"),
		v("three pass convergence with home", "token='a b'/home/tester/.smind/x"),
		v("non-ascii before ghp", "éghp_0123456789abcdef0123"),
		v("non-ascii before sk", "ñsk-abcdefghij1234"),
		v("non-ascii before akia", "üAKIAABCDEFGHIJKLMNOP"),
		v("non-ascii before bearer", "éBearer abcdefghij123"),
		v("nbsp after basic", "Basic\u00a0dXNlcjpwYXNzd29yZA=="),
		v("ideographic space after bearer", "Bearer\u3000abcdefghij123"),
		{Name: "empty home", Input: "/home/tester/x and /Users/bob/y", Home: "", SmindHome: ""},
		{Name: "root home ignored", Input: "/etc/passwd /home/tester", Home: "/", SmindHome: ""},
		{Name: "trailing slash home", Input: "/home/tester/x", Home: "/home/tester/", SmindHome: ""},
	}
}

// vectorCoverage lists every redaction rule (and every alternative inside the
// vendor-token rules) with a pattern for the shape of input that exercises it.
// A rule is covered when some vector's input matches `Shape` AND is changed by
// Redact. `Var` is the Go variable of redact.go the entry covers: the test
// fails when a `re*` variable of redact.go has no entry, so a new rule cannot
// be added without a vector. The list is written into the vector file and the
// Rust crate checks the same entries against the same vectors.
type coverageEntry struct {
	Rule  string `json:"rule"`
	Var   string `json:"var"`
	Shape string `json:"shape"`
}

func vectorCoverage() []coverageEntry {
	e := func(rule, v, shape string) coverageEntry { return coverageEntry{rule, v, shape} }
	return []coverageEntry{
		e("unix user dir", "reUnixUserDir", `(?:/Users/|/home/)[A-Za-z]`),
		e("root user", "reUnixUserDir", `/root\b`),
		e("windows user dir", "reWindowsUser", `[A-Za-z]:[\\/]Users[\\/][A-Za-z]`),
		e("bearer", "reBearer", `(?i)\bbearer\s`),
		e("basic", "reBearer", `(?i)\bbasic\s`),
		e("sk key", "reSK", `\bsk-[A-Za-z0-9]{8}`),
		e("github ghp", "reGitHubToken", `\bghp_`),
		e("github gho", "reGitHubToken", `\bgho_`),
		e("github ghs", "reGitHubToken", `\bghs_`),
		e("github ghu", "reGitHubToken", `\bghu_`),
		e("github ghr", "reGitHubToken", `\bghr_`),
		e("github pat", "reGitHubPat", `\bgithub_pat_`),
		e("aws AKIA", "reAWSKey", `\bAKIA[A-Z0-9]{16}`),
		e("aws ASIA", "reAWSKey", `\bASIA[A-Z0-9]{16}`),
		e("jwt", "reJWT", `\beyJ[A-Za-z0-9_-]{5}`),
		e("key value", "reKeyValue", `(?i)\bpassword\s*[=:]`),
		e("key value quoted", "reKeyValue", `(?i)api_key\s*=\s*'`),
		e("authorization line", "reAuthLine", `(?i)\bauthorization\s*[:=]`),
		e("proxy-authorization line", "reAuthLine", `(?i)proxy-authorization\s*:`),
		e("cookie line", "reAuthLine", `(?i)^cookie\s*:`),
		e("set-cookie line", "reAuthLine", `(?i)set-cookie\s*:`),
		e("scheme token", "reSchemeCred", `(?i)\btoken\s+[A-Za-z0-9]{12}`),
		e("scheme apikey", "reSchemeCred", `(?i)\bapikey\s+[A-Za-z0-9]{12}`),
		e("scheme api-key", "reSchemeCred", `(?i)\bapi-key\s+[A-Za-z0-9]{12}`),
		e("scheme digest", "reSchemeCred", `(?i)\bdigest\s+[A-Za-z0-9]{12}`),
		e("scheme negotiate", "reSchemeCred", `(?i)\bnegotiate\s+[A-Za-z0-9]{12}`),
		e("scheme ntlm", "reSchemeCred", `(?i)\bntlm\s+[A-Za-z0-9]{12}`),
		e("url userinfo", "reURLUserinfo", `://[^/\s:@]+:[^/\s@]+@`),
		e("hex run", "reHexRun", `\b[0-9a-f]{32}\b`),
		e("high entropy run", "reHighEntropy", `[A-Za-z0-9_-]{32}`),
		e("pem block", "rePEM", `-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		e("vendor slack", "reVendorTokens", `\bxox[abprs]-`),
		e("vendor google api key", "reVendorTokens", `\bAIza[0-9A-Za-z_-]{30}`),
		e("vendor gitlab", "reVendorTokens", `\bglpat-`),
		e("vendor npm", "reVendorTokens", `\bnpm_[A-Za-z0-9]{30}`),
		e("vendor google oauth", "reVendorTokens", `\bya29\.`),
		e("vendor stripe sk_live", "reVendorTokens", `\bsk_live_`),
		e("vendor stripe sk_test", "reVendorTokens", `\bsk_test_`),
		e("vendor stripe rk_live", "reVendorTokens", `\brk_live_`),
	}
}

// vectorFile is the committed JSON: the cases, the coverage list, and the
// count (so a truncated or hand-edited file cannot pass quietly on either side).
type vectorFile struct {
	Count    int             `json:"count"`
	Coverage []coverageEntry `json:"coverage"`
	Vectors  []redactVector  `json:"vectors"`
}

func generatedVectors() vectorFile {
	cases := vectorInputs()
	for i := range cases {
		cases[i].Want = Redact(cases[i].Input, cases[i].Home, cases[i].SmindHome)
	}
	return vectorFile{Count: len(cases), Coverage: vectorCoverage(), Vectors: cases}
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(vectorsFile))
	if err != nil {
		t.Fatalf("%v (regenerate with CRASH_UPDATE_VECTORS=1)", err)
	}
	var f vectorFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

// crash-go-redaction-matches-golden-vectors: the committed vectors equal what
// Redact produces today, field for field, with a unique name each and an
// exact count.
func TestCrashGoRedactionMatchesGoldenVectors(t *testing.T) {
	gen := generatedVectors()
	if os.Getenv("CRASH_UPDATE_VECTORS") == "1" {
		data, err := json.MarshalIndent(gen, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsFile, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got := loadVectors(t)
	if got.Count != len(got.Vectors) || got.Count != gen.Count {
		t.Fatalf("count %d, vectors %d, generator %d (regenerate with CRASH_UPDATE_VECTORS=1)", got.Count, len(got.Vectors), gen.Count)
	}
	seen := map[string]bool{}
	for i, c := range gen.Vectors {
		if seen[c.Name] {
			t.Errorf("duplicate vector name %q", c.Name)
		}
		seen[c.Name] = true
		if got.Vectors[i] != c {
			t.Errorf("vector %q differs from what Redact produces now:\n committed want %q\n current      want %q", c.Name, got.Vectors[i].Want, c.Want)
		}
	}
	if len(got.Coverage) != len(gen.Coverage) {
		t.Fatalf("coverage list has %d entries, the generator %d", len(got.Coverage), len(gen.Coverage))
	}
	for i := range gen.Coverage {
		if got.Coverage[i] != gen.Coverage[i] {
			t.Errorf("coverage entry %d differs: %+v vs %+v", i, got.Coverage[i], gen.Coverage[i])
		}
	}
}

// crash-go-vectors-cover-every-rule: every coverage shape matches the input of
// at least one vector that Redact actually changes, and every `re*` regular
// expression variable of redact.go is claimed by some coverage entry.
func TestCrashGoVectorsCoverEveryRule(t *testing.T) {
	f := loadVectors(t)
	for _, c := range f.Coverage {
		re, err := regexp.Compile(c.Shape)
		if err != nil {
			t.Fatalf("coverage %q: %v", c.Rule, err)
		}
		hit := false
		for _, v := range f.Vectors {
			if re.MatchString(v.Input) && v.Want != v.Input {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("rule %q (shape %s): no vector exercises it", c.Rule, c.Shape)
		}
	}
	claimed := map[string]bool{}
	for _, c := range f.Coverage {
		claimed[c.Var] = true
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "redact.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if !regexp.MustCompile(`^re[A-Z]`).MatchString(name.Name) || i >= len(vs.Values) {
				continue
			}
			call, ok := vs.Values[i].(*ast.CallExpr)
			if !ok {
				continue
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "MustCompile" && !claimed[name.Name] {
				t.Errorf("redact.go rule %s has no coverage entry: add a vector and an entry", name.Name)
			}
		}
		return true
	})
}
