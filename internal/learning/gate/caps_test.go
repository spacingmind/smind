package gate

import (
	"strings"
	"testing"
)

func reasons(fs []Finding) []Reason {
	var out []Reason
	for _, f := range fs {
		out = append(out, f.Reason)
	}
	return out
}

func hasReason(fs []Finding, r Reason) bool {
	for _, f := range fs {
		if f.Reason == r {
			return true
		}
	}
	return false
}

func TestGate_RejectsBadName(t *testing.T) {
	bad := []string{
		"go-tests",                           // no prefix
		"Learned-Go-Tests",                   // uppercase
		"learned-go_tests",                   // underscore
		"learned-",                           // prefix only
		"learned-go--tests",                  // double dash
		"learned-" + strings.Repeat("a", 60), // 68 chars total
		"learned-../x",                       // path chars
	}
	for _, name := range bad {
		if fs := CheckName(name); !hasReason(fs, ReasonBadName) {
			t.Errorf("CheckName(%q) = %v, want bad_name", name, reasons(fs))
		}
	}
	if fs := CheckName("learned-go-race-tests"); len(fs) != 0 {
		t.Errorf("CheckName(learned-go-race-tests) = %v, want none", reasons(fs))
	}
}

func TestGate_BadCategory(t *testing.T) {
	bad := []string{
		"go, tests",             // comma
		"go tests",              // space
		"Go",                    // uppercase
		"1go",                   // starts with digit
		"-go",                   // starts with dash
		strings.Repeat("g", 33), // too long
	}
	for _, cat := range bad {
		if fs := CheckCategory(cat); !hasReason(fs, ReasonBadCategory) {
			t.Errorf("CheckCategory(%q) = %v, want bad_category", cat, reasons(fs))
		}
	}
	for _, cat := range []string{"", "go", "testing", "go-race"} {
		if fs := CheckCategory(cat); len(fs) != 0 {
			t.Errorf("CheckCategory(%q) = %v, want none", cat, reasons(fs))
		}
	}
}

func TestGate_RejectsBadDescription(t *testing.T) {
	lim := DefaultLimits()
	bad := []struct {
		name string
		desc string
	}{
		{"empty", "   "},
		{"too long", strings.Repeat("a ", 81)}, // 161 runes after trim
		{"two sentences", "Use when tests fail. Run them again with -race."},
		{"no cue", "Run the tests twice, then push."},
		{"newline", "Use when tests fail.\nRun them again."},
	}
	for _, tc := range bad {
		if fs := CheckDescription(tc.desc, lim); !hasReason(fs, ReasonBadDescription) {
			t.Errorf("CheckDescription(%s) = %v, want bad_description", tc.name, reasons(fs))
		}
	}
	if fs := CheckDescription("Use when a Go test hangs on t.Parallel with shared TempDir.", lim); len(fs) != 0 {
		t.Errorf("good description rejected: %v", reasons(fs))
	}
}

func TestGate_RejectsOversizeBody(t *testing.T) {
	lim := DefaultLimits()
	body26 := strings.TrimSuffix(strings.Repeat("line\n", 26), "\n")
	if fs := CheckBody(body26, lim); !hasReason(fs, ReasonBodyTooLong) {
		t.Errorf("26 lines = %v, want body_too_long", reasons(fs))
	}
	body25 := strings.TrimSuffix(strings.Repeat("line\n", 25), "\n")
	if fs := CheckBody(body25, lim); hasReason(fs, ReasonBodyTooLong) {
		t.Errorf("25 lines rejected: %v", reasons(fs))
	}
	body25blank := body25 + "\n\n\n\n\n"
	if fs := CheckBody(body25blank, lim); hasReason(fs, ReasonBodyTooLong) {
		t.Errorf("25 non-blank + blanks rejected: %v", reasons(fs))
	}
	body4097 := strings.Repeat("a", 4097)
	if fs := CheckBody(body4097, lim); !hasReason(fs, ReasonBodyTooLong) {
		t.Errorf("4097 bytes = %v, want body_too_long", reasons(fs))
	}
}

func TestGate_RejectsFrontmatterKeys(t *testing.T) {
	fs := CheckBody("---\nname: learned-x\ndescription: foo\n---\n\nRun tests.", DefaultLimits())
	if !hasReason(fs, ReasonBadFrontmatter) {
		t.Errorf("leading fence = %v, want bad_frontmatter", reasons(fs))
	}
	fs = CheckBody("Intro line.\n---\nname: learned-x\n---\nBody.", DefaultLimits())
	if !hasReason(fs, ReasonBadFrontmatter) {
		t.Errorf("mid-body fence with keys = %v, want bad_frontmatter", reasons(fs))
	}
	// A --- separator with no frontmatter keys within 6 lines is fine.
	fs = CheckBody("Intro line.\n---\nBody continues here.", DefaultLimits())
	if hasReason(fs, ReasonBadFrontmatter) {
		t.Errorf("bare fence rejected: %v", reasons(fs))
	}
}

func TestGate_RejectsUrlLinks(t *testing.T) {
	bad := []string{
		"See https://example.com/docs",
		"Fetch ftp://host/file",
		"Read file:///etc/passwd",
		"Visit www.example.com now",
		"See [the docs](https://example.com)",
		"Autolink <https://example.com>",
		"An <img src=x> tag",
		"<script>alert(1)</script>",
		"An <a href> link",
		"<details> spoiler </details>",
	}
	for _, text := range bad {
		if fs := CheckLinks(text); !hasReason(fs, ReasonURLLink) {
			t.Errorf("CheckLinks(%q) = %v, want url_link", text, reasons(fs))
		}
	}
	ok := []string{
		"Use the <file> placeholder for paths",
		"Run `go test ./...` with the race flag",
		"div is a word, not a tag",
	}
	for _, text := range ok {
		if fs := CheckLinks(text); len(fs) != 0 {
			t.Errorf("CheckLinks(%q) = %v, want none", text, reasons(fs))
		}
	}
}

func TestGate_RejectsAbsolutePathAndHost(t *testing.T) {
	bad := []struct {
		text   string
		reason Reason
	}{
		{"Leak at /Users/bob/secrets.txt", ReasonAbsPath},
		{"Check /var/log/system.log", ReasonAbsPath},
		{"Read /etc/hosts", ReasonAbsPath},
		{"C:\\Users\\bob\\file.txt", ReasonAbsPath},
		{"Connect to 10.0.0.1 on port 22", ReasonHostName},
		{"See example.com for details", ReasonHostName},
		{"Resolve api.internal first", ReasonHostName},
		{"host myserver.local", ReasonHostName},
	}
	for _, tc := range bad {
		if fs := CheckPathsAndHosts(tc.text); !hasReason(fs, tc.reason) {
			t.Errorf("CheckPathsAndHosts(%q) = %v, want %s", tc.text, reasons(fs), tc.reason)
		}
	}
	ok := []string{
		"Edit package.json and go.mod",
		"Version v1.2.3 is fine",
		"Run foo.test.ts with bun",
		"Paths like ./bin/tool are relative",
		"Home paths like ~/.config/smind stay private",
		"tmp files in the worktree are ok",
	}
	for _, text := range ok {
		if fs := CheckPathsAndHosts(text); len(fs) != 0 {
			t.Errorf("CheckPathsAndHosts(%q) = %v, want none", text, reasons(fs))
		}
	}
}

func TestGate_DetailIsCapped(t *testing.T) {
	fs := CheckLinks("See https://" + strings.Repeat("a", 200))
	if len(fs) != 1 {
		t.Fatalf("want one finding, got %v", fs)
	}
	if n := len([]rune(fs[0].Detail)); n > 80 {
		t.Errorf("Detail is %d runes, want <= 80", n)
	}
}
