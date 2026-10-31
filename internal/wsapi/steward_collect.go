package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/qaframes"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
)

// stewardGit is the slice of *gitstate.Service the collector reads; a fake satisfies it in tests.
type stewardGit interface {
	Get(taskID int64) (gitstate.TaskGit, error)
	PRDetails(taskID int64, focused, refresh bool) (forge.PRDetails, error)
}

// stewardCollector gathers steward.Inputs for one task at one head sha. It is
// read-only: the store is queried, git runs with --no-optional-locks inside
// the task's worktree and nothing is written anywhere. Only the task row, its
// worktree path and the git facts are load-bearing; every other source
// (contract, validations, PR, frames, handoffs, audit) is optional and a
// failure leaves that part of Inputs empty.
type stewardCollector struct {
	db  *store.Store
	val *validation.Service
	git stewardGit // may be nil: no PR and no CI known
}

func newStewardCollector(db *store.Store, val *validation.Service, git stewardGit) *stewardCollector {
	return &stewardCollector{db: db, val: val, git: git}
}

// stewardDevelopRef is the branch a task is normally based on.
const stewardDevelopRef = "origin/develop"

// stewardRefTimeout bounds the diff-base ref lookups.
const stewardRefTimeout = 30 * time.Second

func (c *stewardCollector) collect(ctx context.Context, taskID int64, sha string) (steward.Inputs, error) {
	task, err := c.db.GetTask(taskID)
	if err != nil {
		return steward.Inputs{}, fmt.Errorf("steward collect: %w", err)
	}
	if task.WorktreePath == nil {
		return steward.Inputs{}, fmt.Errorf("steward collect: task %d has no worktree", taskID)
	}
	dir := *task.WorktreePath

	var pr *forge.PR
	if c.git != nil {
		if g, err := c.git.Get(taskID); err == nil && g.PR != nil {
			pr = g.PR
		}
	}
	facts, err := steward.CollectGit(ctx, dir, c.diffBase(ctx, dir, pr, task.BaseSha), task.BaseSha, sha)
	if err != nil {
		return steward.Inputs{}, fmt.Errorf("steward collect: git facts: %w", err)
	}

	in := steward.Inputs{
		Task:        steward.TaskInfo{ID: task.ID, Title: task.Title, HeadSHA: sha, BaseSHA: task.BaseSha},
		Numstat:     facts.Numstat,
		Commits:     facts.Commits,
		Stacking:    facts.Stacking,
		SecretHits:  facts.SecretHits,
		CI:          steward.CIInfo{State: stewardCINone},
		PolicyAudit: c.policyAudit(taskID),
	}
	if task.Branch != nil {
		in.Task.Branch = *task.Branch
	}
	if rec, err := c.val.LatestContract(taskID); err == nil {
		in.HasContract = true
		in.WriteScope = rec.Contract.WriteScope.Paths
		in.PlanFile = rec.Contract.PlanRef
	}
	if views, err := c.val.List(taskID); err == nil && len(views) > 0 {
		v := views[0]
		vi := &steward.ValidationInfo{ID: v.ID, Status: v.Status, CommitSHA: v.CommitSHA, MergeReady: v.MergeReady, ScopeViolation: v.ScopeViolation}
		if v.Verdict != nil {
			for _, it := range v.Verdict.Items {
				if it.Kind != validation.KindMutation {
					continue
				}
				vi.Mutations = append(vi.Mutations, steward.MutationInfo{ID: it.ID, Status: it.Status, Restored: it.Restored != nil && *it.Restored})
			}
		}
		in.Validation = vi
		in.Frames, in.FramesDir = c.frames(v)
	}
	if pr != nil {
		info := &steward.PRInfo{Number: pr.Number, Base: pr.BaseRef, Title: pr.Title}
		ci := steward.CIInfo{State: mapChecksState(pr.Checks)}
		if details, err := c.git.PRDetails(taskID, false, false); err == nil {
			info.Body = details.Body
			ci.Failing = failingCheckNames(details.Checks.Items)
		}
		in.PR, in.CI = info, ci
	}
	in.Handoffs = c.handoffs(taskID)
	return in, nil
}

// diffBase is the ref the diff is taken against: the PR's base branch when
// it resolves in the worktree, else develop, else the task's recorded base.
func (c *stewardCollector) diffBase(ctx context.Context, dir string, pr *forge.PR, baseSha string) string {
	if pr != nil {
		if ref := "origin/" + pr.BaseRef; gitRefResolves(ctx, dir, ref) {
			return ref
		}
	}
	if gitRefResolves(ctx, dir, stewardDevelopRef) {
		return stewardDevelopRef
	}
	return baseSha
}

// gitRefResolves reports whether ref names a commit in dir, via one
// read-only git call. An option-shaped or whitespace-bearing ref is refused
// before it ever reaches git.
func gitRefResolves(ctx context.Context, dir, ref string) bool {
	if !safeStewardRef(ref) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, stewardRefTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir,
		"rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil
}

// safeStewardRef reports whether ref can be passed to git as a revision: not
// empty, not option-shaped (leading "-") and free of whitespace and NUL.
func safeStewardRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	return !strings.ContainsFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || r == 0 })
}

// stewardCINone is the digest's "no CI known" state.
const stewardCINone = "none"

// mapChecksState maps the forge checks rollup onto the digest's CI states;
// anything unknown (or a nil rollup) reads as none.
func mapChecksState(cs *forge.Checks) string {
	if cs == nil {
		return stewardCINone
	}
	switch cs.State {
	case forge.ChecksSuccess, forge.ChecksFailure, forge.ChecksPending:
		return cs.State
	}
	return stewardCINone
}

// failingCheckNames names a PR's failing checks, at most stewardFailingMax.
const stewardFailingMax = 10

func failingCheckNames(items []forge.CheckItem) []string {
	var out []string
	for _, it := range items {
		if it.State != forge.CheckFailure {
			continue
		}
		if len(out) == stewardFailingMax {
			break
		}
		out = append(out, it.Name)
	}
	return out
}

// frames reads the qaframes report of validation v's evidence dir, if it has
// one; each file is stat'ed and a missing one is skipped.
func (c *stewardCollector) frames(v validation.View) ([]steward.FrameInfo, string) {
	dir := c.val.EvidenceDir(v.ID)
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		return nil, ""
	}
	var rep qaframes.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, ""
	}
	var frames []steward.FrameInfo
	for _, fr := range rep.Frames {
		for _, variant := range fr.Variants {
			viewport := 0
			if len(variant.Viewport) > 0 {
				viewport = variant.Viewport[0]
			}
			for _, f := range variant.Files {
				st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f)))
				if err != nil {
					continue
				}
				frames = append(frames, steward.FrameInfo{
					Path: filepath.ToSlash(f), SizeBytes: st.Size(),
					CapturedSHA: v.CommitSHA, Viewport: viewport, Theme: variant.Theme,
				})
			}
		}
	}
	return frames, dir
}

// handoffs reads each child task's newest finished run for its final json
// handoff block. The daemon's own seats (validator and steward children) are
// not Peers and are skipped by their titles; a child without a parsable
// block contributes nothing.
func (c *stewardCollector) handoffs(taskID int64) []steward.HandoffInfo {
	children, err := c.db.ListTasksByParent(taskID)
	if err != nil {
		return nil
	}
	var out []steward.HandoffInfo
	for _, ch := range children {
		title := strings.ToLower(ch.Title)
		if strings.HasPrefix(title, "validate #") || strings.HasPrefix(title, "steward review") {
			continue
		}
		if h, ok := c.childHandoff(ch.ID); ok {
			out = append(out, h)
		}
	}
	return out
}

// stewardChildRunScan bounds how many of a child's runs are considered
// looking for the newest finished one.
const stewardChildRunScan = 20

func (c *stewardCollector) childHandoff(taskID int64) (steward.HandoffInfo, bool) {
	list, err := c.db.ListRunsByTask(taskID, stewardChildRunScan)
	if err != nil {
		return steward.HandoffInfo{}, false
	}
	for _, r := range list {
		if r.Status == string(runs.StatusRunning) {
			continue
		}
		text, err := finalRunText(c.db, r.ID)
		if err != nil {
			return steward.HandoffInfo{}, false
		}
		return parseHandoff(text)
	}
	return steward.HandoffInfo{}, false
}

// finalRunText rebuilds a run's trailing assistant text from its stored
// events: the contiguous text chunks at the end of its history, after the
// turn's trailing bookkeeping events, the same notion the parent notifier
// renders into a child event.
func finalRunText(db *store.Store, runID string) (string, error) {
	events, err := db.ListRunEvents(runID)
	if err != nil {
		return "", err
	}
	end := len(events)
	for end > 0 {
		e, err := runs.DecodeStoredEvent(events[end-1].EventData)
		if err != nil || !bookkeepingEvent(e.Type) {
			break
		}
		end--
	}
	var chunks []string
	for i := end - 1; i >= 0; i-- {
		e, err := runs.DecodeStoredEvent(events[i].EventData)
		if err != nil || e.Type != taskrunner.EventTypeText {
			break
		}
		chunks = append(chunks, e.Text)
	}
	var b strings.Builder
	for i := len(chunks) - 1; i >= 0; i-- {
		b.WriteString(chunks[i])
	}
	return b.String(), nil
}

// bookkeepingEvent reports whether an event type is a run's trailing
// bookkeeping rather than transcript (mirrors runs' child-event renderer).
func bookkeepingEvent(t taskrunner.EventType) bool {
	switch t {
	case taskrunner.EventTypeDone, taskrunner.EventTypeUsage, taskrunner.EventTypeSessionNote,
		taskrunner.EventTypePermissionModeChanged, taskrunner.EventTypeMcpServers, taskrunner.EventTypeRateLimit:
		return true
	}
	return false
}

// parseHandoff reads the first fenced ```json block carrying a status field
// (the Peer handoff shape) out of a run's final text.
func parseHandoff(text string) (steward.HandoffInfo, bool) {
	const open, closeFence = "```json", "```"
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], open)
		if i < 0 {
			return steward.HandoffInfo{}, false
		}
		i += from
		rest := text[i+len(open):]
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			return steward.HandoffInfo{}, false
		}
		if strings.TrimSpace(rest[:nl]) != "" {
			from = i + len(open)
			continue
		}
		body := rest[nl+1:]
		j := strings.Index(body, closeFence)
		if j < 0 {
			return steward.HandoffInfo{}, false
		}
		var raw struct {
			Status      string   `json:"status"`
			NotVerified []string `json:"not_verified"`
			Notes       string   `json:"notes"`
		}
		if err := json.Unmarshal([]byte(body[:j]), &raw); err == nil && raw.Status != "" {
			return steward.HandoffInfo{Status: raw.Status, NotVerified: raw.NotVerified, Notes: raw.Notes}, true
		}
		from = i + len(open) + nl + 1 + j + len(closeFence)
	}
	return steward.HandoffInfo{}, false
}

// policyAudit counts the task's permission decisions: denials (including the
// escalated ones that resolved to a deny) and escalations to a human.
func (c *stewardCollector) policyAudit(taskID int64) steward.PolicyAuditCounts {
	var counts steward.PolicyAuditCounts
	rows, err := c.db.ListPolicyAudit(store.PolicyAuditFilter{TaskID: taskID, Limit: 500})
	if err != nil {
		return counts
	}
	for _, r := range rows {
		switch r.Decision {
		case "deny", "ask_human_deny", "ask_timeout_deny":
			counts.Denied++
		case "ask":
			counts.Escalated++
		}
	}
	return counts
}
