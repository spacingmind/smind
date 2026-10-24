package gitstate

import (
	"strings"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// LookupByHead is the archive preview's one on-demand lookup for a task with
// no stored PR (ADR-0039 O6, narrowed): one `PRsByHead` call for the task's
// branch, and adoption of the PR it finds. It runs only for an explicit human
// action, under the same rules as task.gitRefresh: the per-task floor (5 s)
// and hourly cap (12), the host budget and suspension, the task's backoff, a
// deadline, and single-flight per task. Every failure is booked like any other
// forge failure and then swallowed: the caller keeps today's answer (merged
// unknown or unmerged). It reports whether a PR was adopted.
//
// A PR is accepted only if it is a same-repo PR whose head ref is the task's
// branch and that is open, or merged with a head sha exactly equal to the
// branch's current tip, so a later reuse of the branch name never reads as
// merged. A task that already has an identity costs no call.
func (p *Poller) LookupByHead(taskID int64) bool {
	if p == nil {
		return false
	}
	t, err := p.o.Source.GetTask(taskID)
	if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" || t.PRNumber > 0 ||
		t.Branch == nil || *t.Branch == "" {
		return false
	}
	p.mu.Lock()
	if f := p.lookups[taskID]; f != nil {
		p.mu.Unlock()
		select {
		case <-f.done:
		case <-p.ctx.Done():
		}
		return f.adopted
	}
	f := &wsFlight{done: make(chan struct{})}
	p.lookups[taskID] = f
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.lookups, taskID)
		p.mu.Unlock()
		close(f.done)
	}()
	f.adopted = p.lookupByHead(t)
	return f.adopted
}

func (p *Poller) lookupByHead(t store.Task) bool {
	prov := p.provider()
	if prov == nil || p.ctx.Err() != nil {
		return false
	}
	w, err := p.o.Source.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return false
	}
	now := p.now()
	p.mu.Lock()
	host := p.hostOfLocked(t.WorkspaceID)
	h := p.hosts[host]
	m := p.memLocked(t.ID)
	blocked := (h != nil && (h.suspendedUntil.After(now) || h.budgetUntil.After(now))) ||
		m.st.NextAllowedAt.After(now) || p.refreshLimitLocked(m, now) != nil
	p.mu.Unlock()
	if blocked {
		return false
	}
	ts := []tracked{{t: t}}
	repo, ok, rerr := p.resolveRepo(prov, t.WorkspaceID, w.Path, host, now)
	if rerr != nil {
		p.fail(ts, host, rerr)
		return false
	}
	if !ok || !p.reserve(repo.Host, now) {
		return false
	}
	p.stampForced(t.ID, now)
	branch := *t.Branch
	prs, err := prov.PRsByHead(p.ctx, w.Path, branch)
	if err != nil {
		p.fail(ts, repo.Host, err)
		return false
	}
	tip := p.o.Tip(p.ctx, w.Path, branch)
	var pick *forge.PR
	for i := range prs {
		pr := prs[i]
		if pr.FromFork || pr.HeadRef != branch {
			continue
		}
		switch {
		case pr.State == forge.PRStateOpen:
		case pr.State == forge.PRStateMerged && tip != "" && strings.EqualFold(pr.HeadSha, tip):
		default:
			continue
		}
		if pick == nil || (pick.State != forge.PRStateOpen && pr.State == forge.PRStateOpen) ||
			(pick.State == pr.State && pr.Number > pick.Number) {
			pick = &pr
		}
	}
	if pick == nil {
		return false
	}
	p.adopt(t, *pick, repo, p.now())
	return true
}
