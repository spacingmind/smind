// Command devseed fills a fresh smind home with a believable self-learning
// skills library (ADR-0026) so the Settings -> Skills (learned) UI can be
// run, screenshotted and reviewed without a real learning loop:
//
//	go run ./internal/learning/devseed -home /tmp/smind-learning-demo
//
// It refuses to touch anything that already holds data: -home must be a new
// or empty directory, and never the real smind home (~/.smind or $SMIND_HOME).
// It seeds through the store and approval APIs, prints the commands that start
// a daemon against the seeded home, and exits.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "devseed:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("devseed", flag.ContinueOnError)
	home := fs.String("home", "", "a new or empty directory to seed (never ~/.smind)")
	port := fs.Int("port", 4699, "port printed in the daemon start command")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *home == "" {
		return errors.New("-home is required")
	}
	dir, err := checkHome(*home)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "smind.db"))
	if err != nil {
		return err
	}
	ws, err := seed(st, dir)
	if err != nil {
		st.Close()
		return err
	}
	if err := st.Close(); err != nil {
		return err
	}
	if err := seedCounters(filepath.Join(dir, "smind.db"), ws.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "seeded workspace %q (id %d) in %s\n\n", ws.Title, ws.ID, dir)
	fmt.Fprintf(out, "start a daemon on it (build first: task build):\n\n")
	fmt.Fprintf(out, "  SMIND_HOME=%s SMIND_LISTEN=127.0.0.1:%d ./bin/smind serve\n\n", dir, *port)
	fmt.Fprintf(out, "then open http://127.0.0.1:%d and Settings -> Skills (learned).\n", *port)
	return nil
}

// checkHome returns the cleaned absolute home when it is safe to seed: not the
// real smind home, and absent or empty.
func checkHome(home string) (string, error) {
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	abs = resolve(abs)
	protected := []string{resolve(config.Dir())}
	if uh, err := os.UserHomeDir(); err == nil {
		protected = append(protected, resolve(filepath.Join(uh, ".smind")))
	}
	for _, p := range protected {
		if abs == p {
			return "", fmt.Errorf("%s is the real smind home; pass a throwaway directory", abs)
		}
	}
	entries, err := os.ReadDir(abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	case len(entries) > 0:
		return "", fmt.Errorf("%s is not empty; pass a new or empty directory", abs)
	}
	return abs, nil
}

// resolve follows symlinks in the longest existing prefix of path, so a link
// to ~/.smind (or a not-yet-created child of one) is still recognised.
func resolve(path string) string {
	rest := ""
	for p := path; ; p = filepath.Dir(p) {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		if p == filepath.Dir(p) {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

type evidence struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Seq   int64  `json:"seq"`
	Quote string `json:"quote"`
}

const (
	demoRun     = "run-demo-0001"
	skillTests  = "run-focused-tests"
	skillPush   = "never-force-push"
	skillCommit = "conventional-commits"
)

func evidenceJSON(ev ...evidence) string {
	b, _ := json.Marshal(ev)
	return string(b)
}

func seed(st *store.Store, home string) (store.Workspace, error) {
	wsPath, err := os.MkdirTemp("", "smind-devseed-ws-")
	if err != nil {
		return store.Workspace{}, err
	}
	humanDir := filepath.Join(wsPath, ".claude", "skills", "human-skill")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		return store.Workspace{}, err
	}
	human := "---\nname: human-skill\ndescription: Review a diff for risky changes before committing.\n---\n\nHand-written; smind never edits this.\n"
	if err := os.WriteFile(filepath.Join(humanDir, "SKILL.md"), []byte(human), 0o644); err != nil {
		return store.Workspace{}, err
	}
	ws, err := st.CreateWorkspace(store.Workspace{Path: wsPath, Title: "demo-app", RoutingPolicy: "default"})
	if err != nil {
		return store.Workspace{}, err
	}
	svc := approval.New(approval.Deps{Store: st})
	if err := svc.SetLearningMode(ws.ID, store.LearningModePropose); err != nil {
		return store.Workspace{}, err
	}

	// A task, chat and run the evidence can link to.
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "Fix flaky pkg tests", Status: "created"})
	if err != nil {
		return store.Workspace{}, err
	}
	chat, err := st.CreateChat(task.ID, "main")
	if err != nil {
		return store.Workspace{}, err
	}
	finished := time.Now().UTC().Add(-26 * time.Hour)
	if _, err := st.CreateRun(store.Run{
		ID: demoRun, TaskID: task.ID, ChatID: chat.ID, Provider: "claude", Prompt: "run the tests for pkg/queue",
		Status: "done", StartedAt: finished.Add(-10 * time.Minute), FinishedAt: &finished,
	}); err != nil {
		return store.Workspace{}, err
	}
	ev := evidenceJSON(
		evidence{Kind: gate.EvUserMessage, RunID: demoRun, Seq: 4, Quote: "just run the one package, not everything"},
		evidence{Kind: gate.EvFailureRecovery, RunID: demoRun, Seq: -1, Quote: "go test ./... timed out; go test ./pkg/queue passed"},
	)

	land := func(name, category, description, body, source string) (store.LearnedSkill, error) {
		return st.LandSkill(store.LandSkillInput{
			Skill: store.LearnedSkill{
				WorkspaceID: ws.ID, Name: name, Category: category, Description: description, Body: body,
				Source: source, CreatedByRun: demoRun, Evidence: ev,
			},
			Actor: "human", Reason: "approved from proposal", Action: "approve",
		})
	}

	// Six active skills: one with three versions, one pinned, and counters (seedCounters) that make
	// the rest probation / unmeasured / well-used.
	tests, err := land(skillTests, "workflow", "Run the failing package's tests before the whole suite.",
		"# Focused tests\n\nRun `go test ./<pkg>/...` for the package you changed first.\nOnly run `go test ./...` once that passes.\n", "reflector")
	if err != nil {
		return store.Workspace{}, err
	}
	if tests, err = st.PatchSkill(store.PatchSkillInput{
		ID: tests.ID, ExpectVersion: 1, Actor: "human", Reason: "name the race detector",
		Description: tests.Description,
		Body:        "# Focused tests\n\nRun `go test -race ./<pkg>/...` for the package you changed first.\nOnly run `go test ./...` once that passes.\n",
	}); err != nil {
		return store.Workspace{}, err
	}
	if tests, err = st.PatchSkill(store.PatchSkillInput{
		ID: tests.ID, ExpectVersion: 2, Actor: "curator", Reason: "add the flaky-test rule",
		Description: "Run the failing package's tests, with the race detector, before the whole suite.",
		Body:        "# Focused tests\n\nRun `go test -race ./<pkg>/...` for the package you changed first.\nOnly run `go test ./...` once that passes.\nIf a test is flaky, rerun it with `-count=5` before touching the code.\n",
	}); err != nil {
		return store.Workspace{}, err
	}
	push, err := land(skillPush, "git", "Never force-push a shared branch.",
		"# No force-push\n\nUse `git push --force-with-lease` only on your own feature branch.\nNever force-push `main` or `develop`.\n", "reflector")
	if err != nil {
		return store.Workspace{}, err
	}
	if err := svc.Pin(push.ID, "human"); err != nil {
		return store.Workspace{}, err
	}
	for _, s := range []struct{ name, cat, desc, body, source string }{
		{skillCommit, "git", "Write commit subjects as conventional commits.", "# Commits\n\nUse `type(scope): subject`, 72 characters at most.\nTypes: feat, fix, docs, refactor, test, chore.\n", "curator"},
		{"table-driven-go-tests", "go", "Write Go tests as table-driven subtests.", "# Table tests\n\nOne `[]struct{ name string; ... }` per behaviour.\nRun each case with `t.Run(tc.name, ...)`.\n", "reflector"},
		{"pnpm-not-npm", "tooling", "Use bun in this repo, never npm or pnpm.", "# Package manager\n\nRun `bun install` and `bunx`.\nDo not create a package-lock.json.\n", "reflector"},
		{"explain-before-edit", "workflow", "State the plan before editing more than three files.", "# Plan first\n\nWrite two lines on what changes and why before the first edit.\n", "reflector"},
	} {
		if _, err := land(s.name, s.cat, s.desc, s.body, s.source); err != nil {
			return store.Workspace{}, err
		}
	}
	// One disabled, one archived.
	off, err := land("verbose-logging", "debug", "Add debug logs while investigating a bug.", "# Debug logs\n\nAdd `log.Printf` at each branch while reproducing.\nRemove them before committing.\n", "reflector")
	if err != nil {
		return store.Workspace{}, err
	}
	if err := svc.Disable(off.ID, "human"); err != nil {
		return store.Workspace{}, err
	}
	old, err := land("old-release-steps", "release", "Cut releases by hand with git tag.", "# Releases\n\nTag by hand and push the tag.\n", "reflector")
	if err != nil {
		return store.Workspace{}, err
	}
	if err := svc.Archive(old.ID, "human", "release-please handles releases now"); err != nil {
		return store.Workspace{}, err
	}

	propose := func(kind, source, name, dupOf string, sim float64, items []approval.Item) (store.LearnedProposal, error) {
		return svc.CreateProposal(approval.NewProposal{
			WorkspaceID: ws.ID, Source: source, Kind: kind, Name: name, Items: items,
			Evidence: ev, PossibleDuplicateOf: dupOf, Similarity: sim,
		})
	}
	create := func(name, category, description, body, reason string) approval.Item {
		return approval.Item{
			Intent: gate.Intent{Action: gate.ActionCreate, Name: name, Category: category, Description: description, Body: body, Reason: reason},
			Result: gate.Skill{Name: name, Category: category, Description: description, Body: body},
		}
	}

	// 1. A plain create.
	if _, err := propose("create", "reflector", "cite-file-and-line", "", 0, []approval.Item{create(
		"cite-file-and-line", "workflow", "Cite file and line when explaining code.",
		"# Cite sources\n\nWhen explaining behaviour, name the file and line (`internal/x/y.go:42`).\nQuote the code instead of paraphrasing it.\n",
		"The user asked for file and line references three times in this session.")}); err != nil {
		return store.Workspace{}, err
	}
	// 2. A create flagged as a possible duplicate of a LEARNED skill (similar bodies).
	if _, err := propose("create", "reflector", "test-the-package-first", skillTests, 0.82, []approval.Item{create(
		"test-the-package-first", "workflow", "Run the changed package's tests first, then the rest.",
		"# Package first\n\nRun `go test -race ./<pkg>/...` for the package you changed.\nRun `go test ./...` only after it passes.\nRerun a flaky test with `-count=5`.\n",
		"The user interrupted a full test run and asked for the one package.")}); err != nil {
		return store.Workspace{}, err
	}
	// 3. A create flagged as a duplicate of the HUMAN skill.
	if _, err := propose("create", "reflector", "review-diff-before-commit", "human-skill", 0.71, []approval.Item{create(
		"review-diff-before-commit", "git", "Read the staged diff for risky changes before committing.",
		"# Review the diff\n\nRun `git diff --staged` and read it.\nLook for secrets, debug leftovers and unrelated edits.\n",
		"A permission prompt for a commit was denied because the diff contained a stray debug line.")}); err != nil {
		return store.Workspace{}, err
	}
	// 4. A patch with a visible diff against an active skill.
	pushNow, err := st.GetLearnedSkill(push.ID)
	if err != nil {
		return store.Workspace{}, err
	}
	if _, err := propose("patch", "reflector", skillPush, "", 0, []approval.Item{{
		Intent: gate.Intent{
			Action: gate.ActionPatch, Name: skillPush,
			OldString: "Never force-push `main` or `develop`.\n",
			NewString: "Never force-push `main` or `develop`.\nAsk before force-pushing anything a teammate may have pulled.\n",
			Reason:    "The user stopped a force-push to a release branch and said to ask first.",
		},
		Result:        gate.Skill{Name: skillPush, Description: pushNow.Description, Body: pushNow.Body + "Ask before force-pushing anything a teammate may have pulled.\n"},
		TargetSkillID: pushNow.ID, BaseVersion: pushNow.Version,
	}}); err != nil {
		return store.Workspace{}, err
	}
	// 5. A curator bundle: an umbrella skill that absorbs two others.
	umbrellaBody := "# Git hygiene\n\nUse conventional commits: `type(scope): subject`.\nNever force-push `main` or `develop`; ask before force-pushing shared work.\n"
	if _, err := propose("bundle", "curator", "git-hygiene", "", 0, []approval.Item{
		{
			Intent: gate.Intent{Action: gate.ActionCreate, Name: "git-hygiene", Category: "git", Description: "Commit and push conventions for this repo.", Body: umbrellaBody, Reason: "Two small git skills overlap; one umbrella reads better."},
			Result: gate.Skill{Name: "git-hygiene", Category: "git", Description: "Commit and push conventions for this repo.", Body: umbrellaBody},
		},
		{
			Intent: gate.Intent{Action: gate.ActionDelete, Name: skillCommit, AbsorbedInto: "git-hygiene", Reason: "folded into git-hygiene"},
			Result: gate.Skill{Name: skillCommit},
		},
		{
			Intent: gate.Intent{Action: gate.ActionDelete, Name: "explain-before-edit", AbsorbedInto: "git-hygiene", Reason: "rarely loaded"},
			Result: gate.Skill{Name: "explain-before-edit"},
		},
	}); err != nil {
		return store.Workspace{}, err
	}
	// 6. A rejected proposal, with the note the reflector learns from.
	rej, err := propose("create", "reflector", "always-use-sudo", "", 0, []approval.Item{create(
		"always-use-sudo", "tooling", "Prefix shell commands with sudo.", "# Sudo\n\nPrefix commands with `sudo`.\n", "A command failed with a permission error once.")})
	if err != nil {
		return store.Workspace{}, err
	}
	if err := svc.Reject(rej.ID, "Too broad and unsafe; the failure was a one-off.", "human"); err != nil {
		return store.Workspace{}, err
	}
	_ = home
	return ws, nil
}

// seedCounters sets the usage counters the lifecycle observer would have
// accumulated (the store exposes no setter): probation, unmeasured, well-used.
func seedCounters(dbPath string, workspaceID int64) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC()
	type counters struct {
		name                                 string
		offered, loadedRuns, loads, viaSkill int
		viaRead, reusedAfterPatch            int
		measured                             bool
		lastOffered, lastLoaded              time.Duration // ago; 0 = never
	}
	for _, c := range []counters{
		{skillTests, 24, 17, 22, 15, 7, 1, true, 3 * time.Hour, 5 * time.Hour},
		{skillPush, 31, 20, 26, 26, 0, 0, true, 2 * time.Hour, 26 * time.Hour},
		{skillCommit, 18, 3, 3, 3, 0, 0, true, 6 * time.Hour, 5 * 24 * time.Hour}, // probation: offered a lot, rarely loaded
		{"table-driven-go-tests", 2, 0, 0, 0, 0, 0, false, 4 * time.Hour, 0},      // unmeasured: too few offers
		{"pnpm-not-npm", 12, 9, 11, 4, 7, 0, true, 9 * time.Hour, 12 * time.Hour},
		{"explain-before-edit", 14, 4, 4, 4, 0, 0, true, 8 * time.Hour, 3 * 24 * time.Hour},
		{"verbose-logging", 9, 1, 1, 0, 1, 0, true, 40 * time.Hour, 30 * 24 * time.Hour},
	} {
		var lastOffered, lastLoaded any
		if c.lastOffered > 0 {
			lastOffered = now.Add(-c.lastOffered)
		}
		if c.lastLoaded > 0 {
			lastLoaded = now.Add(-c.lastLoaded)
		}
		if _, err := db.Exec(`UPDATE learned_skills SET offered_runs = ?, loaded_runs = ?, loads = ?, loads_via_skill = ?,
			loads_via_read = ?, reused_after_patch = ?, measured = ?, last_offered_at = ?, last_loaded_at = ?
			WHERE workspace_id = ? AND name = ?`,
			c.offered, c.loadedRuns, c.loads, c.viaSkill, c.viaRead, c.reusedAfterPatch, c.measured, lastOffered, lastLoaded,
			workspaceID, c.name); err != nil {
			return fmt.Errorf("seed counters for %s: %w", c.name, err)
		}
	}
	return nil
}
