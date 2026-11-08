package native

import (
	"fmt"
	"os"
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
