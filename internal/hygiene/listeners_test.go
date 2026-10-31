// Package hygiene holds repository-wide checks that are not about one package's behaviour.
//
// TestNoWildcardListenersInTestsAndHarnesses keeps the test suite from asking for firewall permission. A process that
// listens on 0.0.0.0, [::] or a host-less ":port" makes Windows Defender Firewall (and the macOS application firewall)
// ask the person at the keyboard to allow it, and a rule is left behind for every freshly built test binary. Every
// listener a test or a harness script opens must name its host: 127.0.0.1 or [::1].
package hygiene

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// wildcardForms are the spellings of "every interface" or "host omitted".
var wildcardForms = []struct {
	name string
	re   *regexp.Regexp
}{
	{"0.0.0.0", regexp.MustCompile(`0\.0\.0\.0`)},
	{"[::]", regexp.MustCompile(`\[::\]`)},
	{"host-less address literal", regexp.MustCompile(`["'](:\d+|:%d|:%s)["']`)},
	{"--listen with no host", regexp.MustCompile(`--(grpc-web-)?listen[ =]+["']?:\d`)},
	{"listen: with no host", regexp.MustCompile(`listen:\s*["']?:\d`)},
	{"JS listen(port) with no host", regexp.MustCompile(`\.listen\(\s*[^,()]+\)`)},
	{"Rust unspecified address", regexp.MustCompile(`UNSPECIFIED|Ipv4Addr::new\(0,\s*0,\s*0,\s*0\)`)},
	{"ListenAndServe without a loopback host", regexp.MustCompile(`ListenAndServe\w*\(`)},
}

var loopbackHost = regexp.MustCompile(`127\.0\.0\.1|\[::1\]|localhost`)

// finding is one flagged line.
type finding struct {
	file string // repo-relative, forward slashes
	line int
	text string
	form string
}

// scanLine reports which wildcard forms a line of code uses. Full-line comments are skipped: a comment binds nothing.
func scanLine(line string) []string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "<!--") {
		return nil
	}
	var forms []string
	for _, f := range wildcardForms {
		if !f.re.MatchString(line) {
			continue
		}
		// A call that names a loopback host on the same line is fine (ListenAndServe("127.0.0.1:0", ...)).
		if strings.HasPrefix(f.name, "ListenAndServe") && loopbackHost.MatchString(line) {
			continue
		}
		forms = append(forms, f.name)
	}
	return forms
}

// allow is one reviewed exception: lines of File matching Line, at most Max of them, and why none of them binds beyond loopback.
type allow struct {
	File string
	Line *regexp.Regexp
	Max  int
	Why  string
}

var allowed = []allow{
	{"cmd/smind/direct_cli_test.go", regexp.MustCompile(`0\.0\.0\.0`), 7,
		"configuration editing (`smind set server.listen`), rejected-flag rows, and the refusal test: the daemon refuses a wildcard listen without a password BEFORE it binds and the test asserts nothing listens afterwards. The one test that really needs a daemon beyond loopback (the LAN-address subtest) is skipped unless CI or SMIND_TEST_LAN=1 (lanBindAllowed)."},
	{"internal/config/edit_test.go", regexp.MustCompile(`0\.0\.0\.0|\[::\]|":4648"`), 20,
		"parses, validates and edits configuration values; no socket is opened."},
	{"internal/server/direct_test.go", regexp.MustCompile(`0\.0\.0\.0|\[::\]|":4648"|":5000"`), 9,
		"checks which addresses Binds() would return and drives the guard with synthetic non-loopback peers over httptest (which listens on 127.0.0.1); no wildcard socket is opened."},
	{"internal/accounts/oauth_server_test.go", regexp.MustCompile(`0\.0\.0\.0|\[::\]|":0"`), 6,
		"a table of addresses the OAuth callback server must REFUSE to bind; the loopback check runs before any listen."},
	{"internal/accounts/login_complete_test.go", regexp.MustCompile(`0\.0\.0\.0`), 1,
		"a table of callback addresses that must be refused before any listen."},
	{"internal/service/password_test.go", regexp.MustCompile(`0\.0\.0\.0`), 2,
		"asserts which environment/configuration the service layer writes for a password; no daemon is started."},
	{"desktop/src-tauri/src/ssh_tunnel.rs", regexp.MustCompile(`0\.0\.0\.0`), 1,
		"a macOS-only test (#[cfg(target_os = \"macos\")]) of the wildcard-squatter case, which only macOS permits next to a loopback listener; it is not compiled on Windows or Linux."},
	{"docker/tests/test_image_basics.sh", regexp.MustCompile(`0\.0\.0\.0`), 1,
		"inside the container's own network namespace (the image deliberately listens on 0.0.0.0 and requires a password); the host-side port is published by Docker."},
	{"cmd/smind/host_read_test.go", regexp.MustCompile(`":3"`), 1,
		"a table of malformed host:port specs the CLI must reject (\":3\" has no host); parsed, never bound."},
	{"internal/fleet/mirror/mirror_test.go", regexp.MustCompile(`":7"`), 1,
		"a table of malformed host:port specs the mirror parser must reject; parsed, never bound."},
	{"internal/server/direct_test.go", regexp.MustCompile(`RemoteAddr = "192\.168\.1\."`), 1,
		"builds a synthetic peer address (\"192.168.1.N\" + \":1\") for a request handed straight to the handler; no socket."},
}

// helperDirs are the Go packages that are test support, not product code: their non-_test.go files are scanned too.
var helperDirs = map[string]bool{
	"relaytest": true, "harness": true, "devharness": true, "qaframes": true, "testutil": true, "fakeagent": true,
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "refs": true, "target": true, "dist": true, ".claude": true, ".agents": true,
	"docs": true, "public-docs": true, "docs-site": true, ".vitepress": true, "vendor": true, "hygiene": true,
	"gen": true, "Pods": true, "ios": true, "android": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// scannable reports whether a file is a test, test helper or harness script.
func scannable(rel string) bool {
	slash := filepath.ToSlash(rel)
	base := filepath.Base(slash)
	switch filepath.Ext(base) {
	case ".go":
		if strings.HasSuffix(base, "_test.go") {
			return true
		}
		for _, part := range strings.Split(filepath.Dir(slash), "/") {
			if helperDirs[part] {
				return true
			}
		}
		return false
	case ".rs":
		return strings.HasPrefix(slash, "desktop/")
	case ".mjs", ".cjs", ".js", ".sh", ".ps1":
		// scripts and harnesses; the web and mobile app sources are not harnesses
		return !strings.HasPrefix(slash, "web/packages/ui/src/") && !strings.HasPrefix(slash, "mobile/src/") && !strings.HasPrefix(slash, "mobile/app/")
	}
	return base == "Taskfile.yml"
}

func scanTree(t *testing.T, root string) []finding {
	t.Helper()
	var out []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !scannable(rel) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		r := bufio.NewReaderSize(f, 1<<16)
		for n := 1; ; n++ {
			line, rerr := r.ReadString(0x0a) // no line-length limit: generated fixtures can have megabyte lines
			if len(line) > 0 {
				text := strings.TrimRight(line, "\r\n")
				for _, form := range scanLine(text) {
					if len(text) > 300 {
						text = text[:300] + "..."
					}
					out = append(out, finding{file: filepath.ToSlash(rel), line: n, text: strings.TrimSpace(text), form: form})
				}
			}
			if rerr != nil {
				return nil
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestNoWildcardListenersInTestsAndHarnesses fails on any wildcard or host-less listen address in a test, test helper or
// harness script that is not in the reviewed allow-list, and on any allow-list entry that no longer matches anything.
func TestNoWildcardListenersInTestsAndHarnesses(t *testing.T) {
	root := repoRoot(t)
	findings := scanTree(t, root)

	used := make([]int, len(allowed))
	var bad []string
	for _, f := range findings {
		ok := false
		for i, a := range allowed {
			if a.File == f.file && a.Line.MatchString(f.text) {
				used[i]++
				ok = true
				break
			}
		}
		if !ok {
			bad = append(bad, fmt.Sprintf("%s:%d: %s (%s)", f.file, f.line, f.text, f.form))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d listener address(es) without a loopback host in tests or harnesses. A test or harness that listens on every interface makes the OS firewall ask to allow it. Bind 127.0.0.1 (or [::1]) explicitly; if a wildcard string is only data and nothing binds, add a reviewed entry to the allow-list with the reason:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
	for i, a := range allowed {
		if a.Why == "" {
			t.Errorf("allow-list entry for %s has no reason", a.File)
		}
		if used[i] == 0 {
			t.Errorf("allow-list entry for %s matches nothing any more; remove it", a.File)
		}
		if used[i] > a.Max {
			t.Errorf("%s now has %d matching lines, the reviewed allowance is %d: a new wildcard address needs its own review", a.File, used[i], a.Max)
		}
	}
}

// TestListenerScannerRecognisesTheForms pins what the scanner flags and what it leaves alone, so the guard cannot quietly
// stop seeing a spelling.
func TestListenerScannerRecognisesTheForms(t *testing.T) {
	flagged := []string{
		`l, err := net.Listen("tcp", ":0")`,
		`l, err := net.Listen("tcp", "0.0.0.0:0")`,
		`l, err := net.Listen("tcp4", ":8080")`,
		`srv := &http.Server{Addr: ":4648"}`,
		`addr := fmt.Sprintf(":%d", port)`,
		`l, _ := net.Listen("tcp", "[::]:0")`,
		`http.ListenAndServe(addr, nil)`,
		`http.ListenAndServe(":8080", nil)`,
		`cmd := exec.Command(bin, "relay", "--listen", ":7400")`,
		`args: ["relay", "--listen=:7400"]`,
		`  listen: ":5151"`,
		`env["SMIND_LISTEN"] = "0.0.0.0:4648"`,
		`$Exe config set server.listen "0.0.0.0:$Port"`,
		`server.listen(3000)`,
		`app.listen(PORT);`,
		`let l = TcpListener::bind(("0.0.0.0", port));`,
		`let a = Ipv4Addr::UNSPECIFIED;`,
		`let a = Ipv4Addr::new(0, 0, 0, 0);`,
		`docker run -e SMIND_LISTEN=0.0.0.0:4648 img`,
	}
	for _, line := range flagged {
		if len(scanLine(line)) == 0 {
			t.Errorf("not flagged: %s", line)
		}
	}
	fine := []string{
		`l, err := net.Listen("tcp", "127.0.0.1:0")`,
		`l, err := net.Listen("tcp", "[::1]:0")`,
		`l, err := net.Listen("tcp", "localhost:0")`,
		`l, err := net.Listen("tcp", addr)`,
		`http.ListenAndServe("127.0.0.1:8080", nil)`,
		`addr := fmt.Sprintf("127.0.0.1:%d", port)`,
		`addr := net.JoinHostPort("127.0.0.1", "0")`,
		`server.listen(PORT, '127.0.0.1', r)`,
		`  listen: "127.0.0.1:5151"`,
		`env["SMIND_LISTEN"] = "127.0.0.1:4648"`,
		`$Exe config set server.listen "127.0.0.1:$Port"`,
		`let l = TcpListener::bind(("127.0.0.1", port));`,
		`url := "http://[::1]:4648/healthz"`,
		`// a daemon listening on 0.0.0.0 would prompt`,
		`# listen: ":4648" is the docker default`,
		`host:port`,
		`"example.com:0"`,
		`fmt.Sprintf("host:%d", port)`,
	}
	for _, line := range fine {
		if got := scanLine(line); len(got) != 0 {
			t.Errorf("flagged %v: %s", got, line)
		}
	}
}

// TestAllowListIsPlausible keeps the allow-list from becoming a way around the guard: every entry names an existing file.
func TestAllowListIsPlausible(t *testing.T) {
	root := repoRoot(t)
	for _, a := range allowed {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(a.File))); err != nil {
			t.Errorf("allow-list file %s: %v", a.File, err)
		}
		if strings.Contains(a.File, "*") || strings.HasSuffix(a.File, "/") {
			t.Errorf("allow-list entry %q must name one file, not a directory or a pattern", a.File)
		}
	}
}
