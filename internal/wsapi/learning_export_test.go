package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

// exportFx is a learnFx whose workspace is a real directory and whose export
// home is a temp dir.
type exportFx struct {
	*learnFx
	home, repo string
	skill      store.LearnedSkill
}

func newExportFx(t *testing.T) *exportFx {
	t.Helper()
	f := newLearnFx(t)
	real := func() string {
		d, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	e := &exportFx{learnFx: f, home: real(), repo: real()}
	w, err := f.db.CreateWorkspace(store.Workspace{Path: e.repo, Title: "R", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	f.w = w
	f.api.SetLearning(LearningDeps{Home: e.home})
	e.skill = f.land(lSkillA)
	return e
}

func (e *exportFx) export(target string, force bool) ExportResultView {
	e.t.Helper()
	var res ExportResultView
	e.ok("learning.skills.export", map[string]any{"id": e.skill.ID, "target": target, "force": force}, &res)
	return res
}

// ledger returns the skill's export rows (landing wrote a create row).
func (e *exportFx) ledger() []store.LearnedLedgerEntry {
	e.t.Helper()
	l, err := e.db.ListLedger(e.w.ID, e.skill.ID, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []store.LearnedLedgerEntry
	for _, r := range l {
		if r.Action == "export" {
			out = append(out, r)
		}
	}
	return out
}

func TestLearning_ExportRoundTrip(t *testing.T) {
	t.Parallel()
	e := newExportFx(t)
	want := gate.Render(gate.Skill{Name: lSkillA.Name, Category: lSkillA.Category, Description: lSkillA.Description, Body: lSkillA.Body})
	base := map[string]string{"repo": e.repo, "claude": e.home, "agents": e.home, "codex": e.home}
	sub := map[string]string{"repo": ".agents/skills", "claude": ".claude/skills", "agents": ".agents/skills", "codex": ".codex/skills"}
	for _, target := range exportTargets {
		res := e.export(target, false)
		dir := filepath.Join(base[target], filepath.FromSlash(sub[target]), lSkillA.Name)
		if res.Dir != dir || res.Status != "installed" || len(res.Files) == 0 || res.Skipped == nil || len(res.Skipped) != 0 {
			t.Fatalf("%s: result = %+v, want dir %s", target, res, dir)
		}
		got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil || string(got) != want {
			t.Fatalf("%s: SKILL.md = %q, %v; want gate.Render output", target, got, err)
		}
	}
	// A second export is idempotent; an edited file is left alone unless forced.
	if res := e.export("repo", false); res.Status != "installed" || len(res.Skipped) != 0 {
		t.Fatalf("re-export = %+v", res)
	}
	path := filepath.Join(e.repo, ".agents", "skills", lSkillA.Name, "SKILL.md")
	if err := os.WriteFile(path, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := e.export("repo", false); res.Status != "modified" || len(res.Skipped) != 1 {
		t.Fatalf("export over an edit = %+v", res)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine\n" {
		t.Fatalf("edited file clobbered: %q", b)
	}
	if res := e.export("repo", true); res.Status != "installed" {
		t.Fatalf("forced export = %+v", res)
	}
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Fatalf("forced export did not overwrite: %q", b)
	}
}

func TestLearning_ExportRejectsBadParams(t *testing.T) {
	t.Parallel()
	e := newExportFx(t)
	e.fail("learning.skills.export", map[string]any{"id": e.skill.ID, "target": "nowhere"}, "invalid_params")
	e.fail("learning.skills.export", map[string]any{"id": e.skill.ID}, "invalid_params")
	e.fail("learning.skills.export", map[string]any{"id": 99999, "target": "repo"}, "not_found")
	if len(e.ledger()) != 0 {
		t.Fatalf("failed exports wrote ledger rows: %+v", e.ledger())
	}
}

func TestLearning_ExportRefusesSymlinkedRepoDir(t *testing.T) {
	t.Parallel()
	e := newExportFx(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(e.repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(e.repo, ".agents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ee := e.fail("learning.skills.export", map[string]any{"id": e.skill.ID, "target": "repo"}, "invalid_params")
	if !strings.Contains(ee.Message, "symlink") {
		t.Fatalf("message = %q", ee.Message)
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Fatalf("wrote through the symlink: %v", ents)
	}
	if len(e.ledger()) != 0 {
		t.Fatal("refused export wrote a ledger row")
	}
}

func TestLearning_ExportLedgerRowHasNoBody(t *testing.T) {
	t.Parallel()
	e := newExportFx(t)
	ec := learnEvents(e.learnFx)
	res := e.export("claude", false)
	led := e.ledger()
	if len(led) != 1 {
		t.Fatalf("ledger = %+v", led)
	}
	row := led[0]
	if row.Action != "export" || row.Actor != "human" || row.SkillName != lSkillA.Name || row.WorkspaceID != e.w.ID ||
		row.SkillID == nil || *row.SkillID != e.skill.ID {
		t.Fatalf("row = %+v", row)
	}
	if !strings.Contains(row.Reason, "claude") || !strings.Contains(row.Reason, res.Dir) {
		t.Fatalf("reason %q does not name the target and dir", row.Reason)
	}
	b, _ := json.Marshal(row)
	for _, secret := range []string{lSkillA.Body, "Run the failing test alone", lSkillA.Description} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("ledger row carries skill text %q: %s", secret, b)
		}
	}
	ev := ec.nextEvent(5 * time.Second)
	var p learnedSkillChangedPayload
	decodePayload(t, ev, &p)
	if ev.Topic != TopicLearnedSkillChanged || p.Reason != "exported" || p.SkillID != e.skill.ID || p.WorkspaceID != e.w.ID {
		t.Fatalf("event = %+v %+v", ev, p)
	}
}

func TestLearning_ExportRefusedForRunScoped(t *testing.T) {
	t.Parallel()
	e := newExportFx(t)
	h := e.api.hs["learning.skills.export"]
	_, err := h(context.Background(), &requestContext{runScoped: true}, json.RawMessage(`{"id":`+itoa(e.skill.ID)+`,"target":"claude"}`))
	if ce := codedError(err); err == nil || ce.Code != "forbidden" {
		t.Fatalf("err = %v, want forbidden", err)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("refused export wrote files: %v", err)
	}
	if len(e.ledger()) != 0 {
		t.Fatal("refused export wrote a ledger row")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
