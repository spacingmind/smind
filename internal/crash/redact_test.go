package crash

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCrashRedactsHomeDir: crash-redacts-home-dir — the user's home
// directory never appears in redacted text, in either separator style.
func TestCrashRedactsHomeDir(t *testing.T) {
	home := t.TempDir()
	smindHome := filepath.Join(home, ".smind")
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"slash form", "open " + home + "/notes.txt: no such file", "open ~/notes.txt: no such file"},
		{"backslash form", `read ` + strings.ReplaceAll(home, "/", `\`) + `\notes.txt`, `read ~\notes.txt`},
		{"prefix of another path", home + "/src/main.go:12", "~/src/main.go:12"},
	} {
		if got := Redact(tc.in, home, smindHome); got != tc.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
		if strings.Contains(Redact(tc.in, home, smindHome), home) {
			t.Errorf("%s: redacted text still contains the home %q", tc.name, home)
		}
	}
}

// TestCrashRedactsSmindHome: crash-redacts-smind-home — the smind home
// becomes $SMIND_HOME, more specific than the ~ that replaces the home.
func TestCrashRedactsSmindHome(t *testing.T) {
	home := t.TempDir()
	smindHome := filepath.Join(home, ".smind")
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"under smind home", smindHome + "/crashes/crash-1.json", "$SMIND_HOME/crashes/crash-1.json"},
		{"backslash form", strings.ReplaceAll(smindHome+"/crashes", "/", `\`), `$SMIND_HOME\crashes`},
		{"slash form of a windows path", "C:\\data\\h\\.smind\\config.yaml", "C:\\data\\h\\.smind\\config.yaml"},
	} {
		if got := Redact(tc.in, home, smindHome); got != tc.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestCrashRedactsOtherUserDirs: crash-redacts-other-user-dirs — user
// directory shapes (/Users/x, /home/x, C:\Users\x) become <user dir> even
// when they are not this user's home.
func TestCrashRedactsOtherUserDirs(t *testing.T) {
	home := t.TempDir()
	smindHome := filepath.Join(home, ".smind")
	for _, tc := range []struct{ in, want string }{
		{"/Users/alice/notes.txt", "<user dir>/notes.txt"},
		{"/home/bob/secrets.txt", "<user dir>/secrets.txt"},
		{`C:\Users\carol\file.txt`, `<user dir>\file.txt`},
		{`C:/Users/dan/file.txt`, `<user dir>/file.txt`},
		{"read /Users/eve/", "read <user dir>/"},
		{"/usr/local/share/data", "/usr/local/share/data"},
	} {
		if got := Redact(tc.in, home, smindHome); got != tc.want {
			t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCrashRedactsSecretShapes: crash-redacts-secret-shapes — every secret
// shape from the table is replaced with [redacted], and clean stack text is
// left unchanged.
func TestCrashRedactsSecretShapes(t *testing.T) {
	home := t.TempDir()
	smindHome := filepath.Join(home, ".smind")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bearer", "Authorization: Bearer abc123.DEF-_~+/=", "Authorization: [redacted]"},
		{"sk", "key sk-proj-abcdefghijklmnop123", "key [redacted]"},
		{"ghp", "token ghp_0123456789abcdef", "token [redacted]"},
		{"gho", "gho_ABCDEFGHIJKLMNOP", "[redacted]"},
		{"github_pat", "github_pat_11ABCDEFGHijklMNOP", "[redacted]"},
		{"AKIA", "aws AKIAIOSFODNN7EXAMPLE", "aws [redacted]"},
		{"ASIA", "ASIAIOSFODNN7EXAMPLE", "[redacted]"},
		{"jwt", "auth=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV", "auth=[redacted]"},
		{"key equals", "api_key=supersecret123", "api_key=[redacted]"},
		{"key colon", "password: hunter2", "password: [redacted]"},
		{"url userinfo", "https://user:pass@example.com/x", "https://[redacted]@example.com/x"},
		{"hex run", "digest deadbeefdeadbeefdeadbeefdeadbeef", "digest [redacted]"},
		{"high entropy", "run AbCdEf1234567890AbCdEf1234567890AbCdEf12", "run [redacted]"},
		{"stack frame unchanged", "github.com/spacingmind/smind/internal/wsapi.(*conn).dispatch(0x14000123456, ...)", "github.com/spacingmind/smind/internal/wsapi.(*conn).dispatch(0x14000123456, ...)"},
		{"lowercase name unchanged", "abcdefghijklmnopqrstuvwxyzijklmno", "abcdefghijklmnopqrstuvwxyzijklmno"},
		{"plain text unchanged", "index out of range [5] with length 3", "index out of range [5] with length 3"},
	}
	for _, tc := range cases {
		if got := Redact(tc.in, home, smindHome); got != tc.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
