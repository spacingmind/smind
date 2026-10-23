package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/workspace"
)

// task.prPreview, task.createPr and task.updatePr (ADR-0039 section 5).
//
// Every precondition is read from one place, gitstate's createPr/updatePr
// actions, on a freshly recomputed snapshot: the preview shows the blocker,
// the RPCs refuse with the same code and sentence, so the three cannot
// disagree. Failures are typed (*forge.PRError, *forge.Error): a code, a fixed
// sentence, and a scrubbed detail that never holds argv, a token or an env
// value.

const (
	maxPRTitleRunes = 256
	maxPRBodyBytes  = 64 << 10
	// prActionTimeout bounds one whole Create/Update PR (fetch 20 s, push 60 s,
	// the forge calls 20 s each) once its preconditions passed.
	prActionTimeout = 3 * time.Minute

	prBodyBulletLimit = 20
	prSubjectRunes    = 200
)

// prHost holds what the three handlers share.
type prHost struct {
	wm       *workspace.Manager
	svc      *gitstate.Service
	provider func() forge.Provider

	mu   sync.Mutex
	busy map[int64]bool
}

func prHandlers(wm *workspace.Manager, svc *gitstate.Service, gh *githubHolder) map[string]handlerFunc {
	h := &prHost{wm: wm, svc: svc, busy: map[int64]bool{}, provider: func() forge.Provider {
		if gh == nil || gh.c == nil {
			return nil
		}
		return gh.c
	}}
	return map[string]handlerFunc{
		"task.prPreview": h.preview,
		"task.createPr":  h.create,
		"task.updatePr":  h.update,
		// Push and Update from base (sync_handlers.go) share the busy guard.
		"task.push":           h.push,
		"task.updateFromBase": h.updateFromBase,
	}
}

// begin marks taskID busy; ok is false when a Create/Update is already running
// for it (the caller answers pr_busy).
func (h *prHost) begin(taskID int64) (end func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.busy[taskID] {
		return nil, false
	}
	h.busy[taskID] = true
	return func() {
		h.mu.Lock()
		delete(h.busy, taskID)
		h.mu.Unlock()
	}, true
}

func refuseRunScoped(method string, rc *requestContext) error {
	if rc != nil && rc.runScoped {
		return &rpcCodeError{code: codeForbidden, msg: method + " is not available to agents"}
	}
	return nil
}

// --- task.prPreview ---

type prRef struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}

type prHead struct {
	Branch   string `json:"branch"` // what Create PR pushes
	Ref      string `json:"ref"`    // the task's own branch
	Strategy string `json:"strategy"`
}

type prCommit struct {
	Sha     string `json:"sha"`
	Subject string `json:"subject"`
}

type prUncommitted struct {
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
}

type prDefaults struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Draft bool   `json:"draft"`
}

type prForgeInfo struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
	Host string `json:"host"`
}

// prDiffStat is the committed change the pull request will contain.
type prDiffStat struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

type prBlocker struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// prPreviewResult is the result of task.prPreview.
type prPreviewResult struct {
	Base        prRef         `json:"base"`
	Head        prHead        `json:"head"`
	Commits     []prCommit    `json:"commits"`
	CommitCount int           `json:"commitCount"`
	Diff        *prDiffStat   `json:"diff,omitempty"`
	Uncommitted prUncommitted `json:"uncommitted"`
	ExistingPR  *forge.PR     `json:"existingPr,omitempty"`
	Defaults    prDefaults    `json:"defaults"`
	Forge       *prForgeInfo  `json:"forge,omitempty"`
	Blockers    []prBlocker   `json:"blockers"`
}

func (h *prHost) preview(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	var p struct {
		TaskID     int64  `json:"taskId"`
		BaseBranch string `json:"baseBranch"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("task.prPreview: invalid params: %w", err)
	}
	t, err := h.wm.GetTask(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.prPreview: %w", err)
	}
	g, err := h.svc.Fresh(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.prPreview: task %d not found or archived", p.TaskID)
	}
	res := prPreviewResult{
		Commits: []prCommit{}, Blockers: []prBlocker{}, ExistingPR: g.PR,
		Defaults: prDefaults{Title: defaultPRTitle(t.Title, t.ID), Draft: true},
	}
	if g.Worktree != nil {
		res.Uncommitted = prUncommitted{Staged: g.Worktree.Staged, Unstaged: g.Worktree.Unstaged, Untracked: g.Worktree.Untracked}
	}
	if a := g.Actions["createPr"]; !a.Enabled {
		res.Blockers = append(res.Blockers, prBlocker{Code: a.Code, Reason: a.Reason})
	}
	res.Forge = h.forgeInfo(t.WorkspaceID, t.PRRepo, t.PRURL)

	info, err := h.wm.PRPreview(ctx, p.TaskID, p.BaseBranch)
	var pe *forge.PRError
	if errors.As(err, &pe) && (pe.Code == forge.CodeTaskNoWorktree || pe.Code == forge.CodeBaseUnknown) {
		// Nothing local to show, and the blocker already says why.
		res.Defaults.Body = defaultPRBody(nil, 0, t.ID)
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	res.Base = prRef{Ref: info.Base, Sha: info.BaseSha}
	res.Head = prHead{Branch: info.Head, Ref: info.Branch, Strategy: info.Strategy}
	res.CommitCount = info.CommitCount
	if info.CommitCount > 0 {
		res.Diff = &prDiffStat{Files: info.Diff.Files, Insertions: info.Diff.Insertions, Deletions: info.Diff.Deletions}
	}
	for _, c := range info.Commits {
		res.Commits = append(res.Commits, prCommit{Sha: c.SHA, Subject: c.Subject})
	}
	res.Defaults.Body = defaultPRBody(info.Commits, info.CommitCount, t.ID)
	return res, nil
}

// forgeInfo names the forge repo from what is already known: the repo the
// poller resolved for the workspace, else the task's stored PR identity. It
// never calls the forge.
func (h *prHost) forgeInfo(workspaceID int64, prRepo, prURL string) *prForgeInfo {
	prov := h.provider()
	if prov == nil {
		return nil
	}
	if r, ok := h.svc.ForgeRepo(workspaceID); ok {
		return &prForgeInfo{Name: prov.Name(), Repo: r.NameWithOwner, Host: r.Host}
	}
	if prRepo != "" && prURL != "" {
		if u, err := url.Parse(prURL); err == nil && u.Host != "" {
			return &prForgeInfo{Name: prov.Name(), Repo: prRepo, Host: u.Host}
		}
	}
	return nil
}

// defaultPRTitle is the task's title on one line, clipped to the title limit;
// an untitled task is "smind task <id>". Deterministic: no model writes it.
func defaultPRTitle(title string, id int64) string {
	title = strings.Join(strings.FieldsFunc(title, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if title == "" {
		return fmt.Sprintf("smind task %d", id)
	}
	return clipRunes(title, maxPRTitleRunes)
}

// defaultPRBody is the deterministic description: a Summary list of the
// commits' subjects (twenty at most, then "…and N more") and the footer that
// says where the PR came from.
func defaultPRBody(commits []workspace.PRCommit, count int, id int64) string {
	var b strings.Builder
	b.WriteString("## Summary\n")
	for i, c := range commits {
		if i == prBodyBulletLimit {
			break
		}
		b.WriteString("- " + clipRunes(cleanSubject(c.Subject), prSubjectRunes) + "\n")
	}
	if count > prBodyBulletLimit {
		fmt.Fprintf(&b, "- …and %d more\n", count-prBodyBulletLimit)
	}
	fmt.Fprintf(&b, "\nOpened from smind task #%d.", id)
	return b.String()
}

func cleanSubject(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == 0 || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s))
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// --- task.createPr ---

// validatePRInput checks the user's text before anything runs.
func validatePRInput(title, body, baseBranch string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxPRTitleRunes || strings.ContainsAny(title, "\x00\n\r") || !utf8.ValidString(title) {
		return "", forge.NewPRError(forge.CodeInvalidTitle, "")
	}
	if len(body) > maxPRBodyBytes || strings.ContainsRune(body, 0) || !utf8.ValidString(body) {
		return "", forge.NewPRError(forge.CodeInvalidBody, "")
	}
	if len(baseBranch) > 255 || strings.ContainsRune(baseBranch, 0) {
		return "", forge.NewPRError(forge.CodeInvalidBranch, "")
	}
	return title, nil
}

// blocked turns a disabled action into the RPC error: the action's code and
// its sentence, plus {url, number} for pr_exists.
func blocked(a gitstate.Action, g gitstate.TaskGit) *forge.PRError {
	e := &forge.PRError{Code: a.Code, Message: a.Reason}
	if a.Code == forge.CodePRExists && g.PR != nil {
		e.With("url", g.PR.URL).With("number", g.PR.Number)
	}
	return e
}

// prCreateResult is the result of task.createPr.
type prCreateResult struct {
	PR forge.PR `json:"pr"`
}

func (h *prHost) create(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	if err := refuseRunScoped("task.createPr", rc); err != nil {
		return nil, err
	}
	var p struct {
		TaskID     int64  `json:"taskId"`
		BaseBranch string `json:"baseBranch"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("task.createPr: invalid params: %w", err)
	}
	title, err := validatePRInput(p.Title, p.Body, p.BaseBranch)
	if err != nil {
		return nil, err
	}
	end, ok := h.begin(p.TaskID)
	if !ok {
		return nil, forge.NewPRError(forge.CodePRBusy, "")
	}
	defer end()
	// The push and the forge call must not be cut off half way by the client
	// going away: the PR would exist with no identity stored for it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prActionTimeout)
	defer cancel()

	g, err := h.svc.Fresh(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.createPr: task %d not found or archived", p.TaskID)
	}
	if a := g.Actions["createPr"]; !a.Enabled {
		return nil, blocked(a, g)
	}
	t, err := h.wm.GetTask(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.createPr: %w", err)
	}
	ws, err := h.wm.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("task.createPr: %w", err)
	}
	prov := h.provider()
	if prov == nil {
		return nil, forge.NewError(forge.CodeCLIMissing, "")
	}

	pushed, err := h.wm.PushForPR(ctx, p.TaskID, p.BaseBranch)
	if err != nil {
		return nil, err
	}
	pr, err := prov.CreatePR(ctx, ws.Path, forge.CreatePRInput{
		Base: pushed.Base, Head: pushed.Branch, Title: title, Body: p.Body, Draft: p.Draft,
	})
	if err != nil {
		return nil, err
	}
	// The head is the branch smind pushed, whatever the forge echoes back.
	pr.HeadRef = pushed.Branch
	h.noteCalls(pr, 2) // gh pr create + gh pr view
	if err := h.svc.AdoptPR(p.TaskID, pr); err != nil {
		return nil, fmt.Errorf("task.createPr: the pull request was opened (%s) but could not be recorded: %w", pr.URL, err)
	}
	return prCreateResult{PR: pr}, nil
}

// noteCalls books n explicit forge calls against the PR's host budget.
func (h *prHost) noteCalls(pr forge.PR, n int) {
	if u, err := url.Parse(pr.URL); err == nil && u.Host != "" {
		h.svc.NoteForgeCalls(u.Host, n)
	}
}

// --- task.updatePr ---

type prPushed struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// prUpdateResult is the result of task.updatePr.
type prUpdateResult struct {
	PR     forge.PR `json:"pr"`
	Pushed prPushed `json:"pushed"`
}

func (h *prHost) update(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	if err := refuseRunScoped("task.updatePr", rc); err != nil {
		return nil, err
	}
	var p struct {
		TaskID int64 `json:"taskId"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("task.updatePr: invalid params: %w", err)
	}
	end, ok := h.begin(p.TaskID)
	if !ok {
		return nil, forge.NewPRError(forge.CodePRBusy, "")
	}
	defer end()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prActionTimeout)
	defer cancel()

	g, err := h.svc.Fresh(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.updatePr: task %d not found or archived", p.TaskID)
	}
	if a := g.Actions["updatePr"]; !a.Enabled {
		return nil, blocked(a, g)
	}
	t, err := h.wm.GetTask(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task.updatePr: %w", err)
	}
	ws, err := h.wm.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("task.updatePr: %w", err)
	}
	head := t.PRHeadRef
	if head == "" {
		head = g.PR.HeadRef
	}
	if head == "" || t.Branch == nil {
		return nil, forge.NewPRError(forge.CodeInvalidBranch, "")
	}
	if strings.HasPrefix(head, forge.CherryPickBranchPrefix) {
		return nil, forge.UpdateUnsupported(head)
	}
	if err := h.wm.PushUpdate(ctx, p.TaskID, head); err != nil {
		return nil, err
	}

	// The push is done. Refresh the snapshot so the chip shows the new head;
	// a forge failure now must not hide that the push happened, so the cached
	// snapshot is returned instead (the poller catches up).
	pr := *g.PR
	if prov := h.provider(); prov != nil && t.PRNumber > 0 {
		if fresh, err := prov.PR(ctx, ws.Path, int(t.PRNumber)); err == nil {
			pr = fresh
			h.noteCalls(pr, 1)
			if err := h.svc.RecordPR(p.TaskID, pr); err != nil {
				return nil, fmt.Errorf("task.updatePr: pushed, but the pull request could not be recorded: %w", err)
			}
		}
	}
	return prUpdateResult{PR: pr, Pushed: prPushed{From: *t.Branch, To: head}}, nil
}
