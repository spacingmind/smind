package native

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	rolloutTS    = "2026-11-05T09-00-00"
	rolloutUUID1 = "01a1bbbb-bbbb-7ccc-8ddd-eeeeeeeeeee1"
	rolloutUUID2 = "01a1bbbb-bbbb-7ccc-8ddd-eeeeeeeeeee2"
)

// legacyRollout is a synthetic legacy-mode rollout: the same turn as the item
// form, written as user_message / agent_message (no item_completed).
func legacyRollout(thread string) string {
	return strings.Join([]string{
		`{"timestamp":"2026-11-05T08:59:59.000Z","type":"session_meta","payload":{"id":"` + thread + `","cwd":"/work/repo","cli_version":"0.80.0"}}`,
		`{"timestamp":"2026-11-05T09:00:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1793869200}}`,
		`{"timestamp":"2026-11-05T09:00:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"Summarize the repository","images":[],"local_images":[],"text_elements":[]}}`,
		`{"timestamp":"2026-11-05T09:00:03.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Summary text.","phase":"final_answer","memory_citation":null}}`,
		`{"timestamp":"2026-11-05T09:00:05.000Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t1"}}`,
		`{"timestamp":"2026-11-05T09:10:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2","started_at":1793869800}}`,
		`{"timestamp":"2026-11-05T09:10:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"` + targetPrompt + `"}}`,
		`{"timestamp":"2026-11-05T09:10:03.000Z","type":"event_msg","payload":{"type":"agent_message","message":"I will run the tests. ","phase":"commentary"}}`,
		`{"timestamp":"2026-11-05T09:10:08.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Done: fixed.","phase":"final_answer"}}`,
		`{"timestamp":"2026-11-05T09:10:09.000Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t2"}}`,
		"",
	}, "\n")
}

func codexTexts(t *testing.T, res Result) string {
	t.Helper()
	if !res.OK() {
		t.Fatalf("rebuild refused: %q (%s)", res.Reason, res.Detail)
	}
	var b strings.Builder
	for _, ev := range res.Events {
		b.WriteString(ev.Text)
	}
	return b.String()
}

func writeRollout(t *testing.T, e *env, name, body string) string {
	t.Helper()
	p := filepath.Join(e.root, "sessions", "2026", "11", "05", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCodexLegacyRolloutRebuilds(t *testing.T) {
	e := newCodex(t)
	os.Remove(e.file)
	writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+".jsonl", legacyRollout(codexThread))
	e.ref.TranscriptPath = ""
	if got := codexTexts(t, e.rebuild()); got != "I will run the tests. Done: fixed." {
		t.Fatalf("legacy text = %q", got)
	}
	// the other turn of the same file is still told apart by its prompt
	e.ref.PromptSha256 = sha("Summarize the repository")
	e.ref.TurnWindow = TurnWindow{StartedAt: at("2026-11-05T09:00:00Z"), FinishedAt: at("2026-11-05T09:00:05Z")}
	if got := codexTexts(t, e.rebuild()); got != "Summary text." {
		t.Fatalf("legacy first turn = %q", got)
	}
}

// A turn that has both forms is read once, from the item form.
func TestCodexLegacyDoesNotDuplicateItemForm(t *testing.T) {
	e := newCodex(t)
	b, _ := os.ReadFile(e.file)
	extra := `{"timestamp":"2026-11-05T09:10:04.000Z","type":"event_msg","payload":{"type":"agent_message","message":"DUPLICATE"}}` + "\n"
	lines := strings.SplitAfter(string(b), "\n")
	var out []string
	for _, l := range lines {
		out = append(out, l)
		if strings.Contains(l, `"turn_id":"turn-2"`) && strings.Contains(l, `"AgentMessage"`) && !strings.Contains(strings.Join(out[:len(out)-1], ""), "DUPLICATE") {
			out = append(out, extra)
		}
	}
	os.WriteFile(e.file, []byte(strings.Join(out, "")), 0o600)
	if got := codexTexts(t, e.rebuild()); strings.Contains(got, "DUPLICATE") || got != "I will run the tests. Done: fixed." {
		t.Fatalf("text = %q", got)
	}
}

func TestCodexRevertedThreadRolloutName(t *testing.T) {
	setup := func(t *testing.T) *env {
		e := newCodex(t)
		os.Remove(e.file)
		e.ref.TranscriptPath = ""
		return e
	}
	t.Run("suffixed name is found", func(t *testing.T) {
		e := setup(t)
		writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+"_"+rolloutUUID1+".jsonl", legacyRollout(codexThread))
		if got := codexTexts(t, e.rebuild()); got != "I will run the tests. Done: fixed." {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("archived suffixed name is found", func(t *testing.T) {
		e := setup(t)
		p := filepath.Join(e.root, "archived_sessions", "rollout-"+rolloutTS+"-"+codexThread+"_"+rolloutUUID1+".jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(legacyRollout(codexThread)), 0o600)
		if got := codexTexts(t, e.rebuild()); got == "" {
			t.Fatal("no text")
		}
	})
	t.Run("several rollouts: the newest wins", func(t *testing.T) {
		e := setup(t)
		old := writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+".jsonl", strings.ReplaceAll(legacyRollout(codexThread), "Done: fixed.", "OLD"))
		neu := writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+"_"+rolloutUUID2+".jsonl", legacyRollout(codexThread))
		os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
		os.Chtimes(neu, time.Now(), time.Now())
		if got := codexTexts(t, e.rebuild()); got != "I will run the tests. Done: fixed." {
			t.Fatalf("text = %q", got)
		}
	})
	// None of these may be read as a rollout of codexThread, even though a file
	// that parses fine sits under the right directory.
	notMine := map[string]string{
		"suffix that is not a UUID":          "rollout-" + rolloutTS + "-" + codexThread + "_notauuid.jsonl",
		"suffix climbing":                    "rollout-" + rolloutTS + "-" + codexThread + "_..-x.jsonl",
		"two suffixes":                       "rollout-" + rolloutTS + "-" + codexThread + "_" + rolloutUUID1 + "_" + rolloutUUID2 + ".jsonl",
		"another thread ending with the id":  "rollout-" + rolloutTS + "-x-" + codexThread + ".jsonl",
		"another thread extending the id":    "rollout-" + rolloutTS + "-" + codexThread + "9.jsonl",
		"another thread, id with underscore": "rollout-" + rolloutTS + "-" + codexThread + "_" + rolloutUUID1 + "x.jsonl",
		"backup extension":                   "rollout-" + rolloutTS + "-" + codexThread + "_" + rolloutUUID1 + ".jsonl.bak",
		"free-form timestamp":                "rollout-anything-" + codexThread + ".jsonl",
	}
	for name, file := range notMine {
		t.Run("refused: "+name, func(t *testing.T) {
			e := setup(t)
			writeRollout(t, e, file, legacyRollout(codexThread))
			if res := e.rebuild(); res.OK() {
				t.Fatalf("%s was read as the rollout of %s", file, codexThread)
			}
		})
	}
	t.Run("suffix of the asked-for id cannot select a longer thread", func(t *testing.T) {
		// the thread asked for is "<codexThread>_<uuid>": only its own file matches, not the base thread's
		e := setup(t)
		writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+".jsonl", legacyRollout(codexThread))
		e.ref.SessionID = codexThread + "_" + rolloutUUID1
		if res := e.rebuild(); res.OK() {
			t.Fatal("the base thread's file answered for a different thread id")
		}
	})
	t.Run("a symlinked suffixed rollout leaving the root is refused", func(t *testing.T) {
		e := setup(t)
		outside := filepath.Join(t.TempDir(), "x.jsonl")
		os.WriteFile(outside, []byte(legacyRollout(codexThread)), 0o600)
		link := filepath.Join(e.root, "sessions", "2026", "11", "05", "rollout-"+rolloutTS+"-"+codexThread+"_"+rolloutUUID1+".jsonl")
		os.MkdirAll(filepath.Dir(link), 0o755)
		if err := os.Symlink(outside, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		if res := e.rebuild(); res.OK() {
			t.Fatal("symlink escape accepted")
		}
	})
	t.Run("ids that could climb are refused", func(t *testing.T) {
		e := setup(t)
		writeRollout(t, e, "rollout-"+rolloutTS+"-"+codexThread+".jsonl", legacyRollout(codexThread))
		for _, id := range []string{codexThread + "_../x", codexThread + "_*", codexThread + "/../" + codexThread} {
			ref := e.ref
			ref.SessionID = id
			if res := e.rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman}); res.OK() {
				t.Errorf("thread id %q accepted", id)
			}
		}
	})
}
