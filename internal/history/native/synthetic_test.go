package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeSyntheticUserRecordsAreNotPrompts: a compaction in the middle of a
// turn writes a user record with string content (a summary of the earlier
// turns). It is not something the human typed, so it must neither end the turn
// being rebuilt nor appear in it.
func TestClaudeSyntheticUserRecordsAreNotPrompts(t *testing.T) {
	base := newClaude(t).rebuild()
	if !base.OK() || len(base.Events) == 0 {
		t.Fatalf("baseline: %q", base.Reason)
	}
	const summary = `{"parentUuid":"x","isSidechain":false,"promptId":"p","type":"user","message":{"role":"user","content":"This session is being continued from a previous conversation. SUMMARY-MARKER"},%s"uuid":"u","timestamp":"2026-11-05T09:10:03.200Z","userType":"external","entrypoint":"cli","cwd":"/work/repo","sessionId":"` + claudeSession + `","version":"2.1.310"}`
	for name, flags := range map[string]string{
		"compact summary":          `"isVisibleInTranscriptOnly":true,"isCompactSummary":true,`,
		"compact summary only":     `"isCompactSummary":true,`,
		"transcript-only user row": `"isVisibleInTranscriptOnly":true,`,
		"meta":                     `"isMeta":true,`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newClaude(t)
			b, err := os.ReadFile(e.file)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(b), "\n")
			var out []string
			inserted := false
			for _, l := range lines {
				out = append(out, l)
				// after the sidechain record, still inside the 09:10:00 turn
				if !inserted && strings.Contains(l, `"timestamp":"2026-11-05T09:10:03.500Z"`) {
					out = append(out, fmt.Sprintf(summary, flags))
					inserted = true
				}
			}
			if !inserted {
				t.Fatal("fixture anchor not found")
			}
			if err := os.WriteFile(e.file, []byte(strings.Join(out, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			res := e.rebuild()
			if !res.OK() {
				t.Fatalf("rebuild: %q (%s)", res.Reason, res.Detail)
			}
			if len(res.Events) != len(base.Events) {
				t.Fatalf("%d events with a synthetic user record, %d without (the turn was cut or padded)", len(res.Events), len(base.Events))
			}
			for i := range res.Events {
				if view(res.Events[i]) != view(base.Events[i]) {
					t.Fatalf("event %d differs: %q vs %q", i, view(res.Events[i]), view(base.Events[i]))
				}
				if strings.Contains(res.Events[i].Text, "SUMMARY-MARKER") {
					t.Fatalf("the summary leaked into event %d", i)
				}
			}
		})
	}
}

// TestClaudeProjectDirMatchesTheCLI: the expected names are what the CLI's own
// sanitiser produces (its JS source run on the same strings): a name over 200
// is cut and carries a base36 hash of the unsanitised cwd; an astral character
// is two dashes.
func TestClaudeProjectDirMatchesTheCLI(t *testing.T) {
	long := "/Users/dev/" + strings.Repeat("projects/", 30) + "app.v2/é😀src"
	cases := []struct{ cwd, want string }{
		{"/work/repo", "-work-repo"},
		{"/Users/x/.smind/wt", "-Users-x--smind-wt"},
		{"/Users/x/😀y", "-Users-x---y"},
		{long, "-Users-dev" + strings.Repeat("-projects", 21) + "--2i0l7y"},
		{"/" + strings.Repeat("a/", 150), strings.Repeat("-a", 100) + "-1o1hgj"},
		// exactly 200 is not cut
		{"/" + strings.Repeat("b", 199), "-" + strings.Repeat("b", 199)},
	}
	for _, c := range cases {
		if got := claudeProjectDir(c.cwd); got != c.want {
			t.Errorf("claudeProjectDir(%.40q...) = %q, want %q", c.cwd, got, c.want)
		}
	}
	// 201 chars: cut to 200 and hashed
	if got := claudeProjectDir("/" + strings.Repeat("b", 200)); len(got) <= 200 || !strings.HasPrefix(got, "-"+strings.Repeat("b", 199)+"-") {
		t.Errorf("201-char cwd not cut and hashed: %q", got)
	}
}

// TestClaudeLongCwdTranscriptFound: a session under a long cwd resolves at the
// capped name, and a recorded path that disagrees with it is still refused.
func TestClaudeLongCwdTranscriptFound(t *testing.T) {
	cwd := "/work/" + strings.Repeat("deep/", 60) + "repo"
	e := newClaude(t)
	file := filepath.Join(e.root, "projects", claudeProjectDir(cwd), claudeSession+".jsonl")
	copyFile(t, "testdata/claude/session.jsonl", file)
	e.ref.Cwd, e.ref.TranscriptPath = cwd, file
	if res := e.rebuild(); !res.OK() {
		t.Fatalf("long-cwd transcript not found: %q (%s)", res.Reason, res.Detail)
	}
	e.ref.TranscriptPath = filepath.Join(e.root, "projects", strings.Repeat("-work-deep", 20), claudeSession+".jsonl")
	if res := e.rebuild(); res.OK() {
		t.Fatal("a recorded path that is not the derived one was accepted")
	}
}
