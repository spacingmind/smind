package automation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

const (
	// AutomationHourlyCalls is the share of a host's 240 hourly forge calls the
	// automations may use (ADR-0044 section 2.2); the PR poller keeps the rest.
	AutomationHourlyCalls = 60
	hostHourlyCalls       = 240
	// repoFloor is the minimum gap between two forge calls for one repo
	// (ADR-0039 section 4).
	repoFloor    = 15 * time.Second
	rateLimitFor = 10 * time.Minute
	// automationsLedgerPrefix keys the automations' own hourly counter in the
	// forge_budget table, next to the real host's row.
	automationsLedgerPrefix = "automations:"
	pollRunLimit            = 20
)

// Ledger is the slice of *store.Store the GitHub source books calls through.
type Ledger interface {
	AddForgeCalls(host string, n int, now time.Time) (int, error)
	GetForgeBudget(host string, now time.Time) (int, time.Time, error)
}

// GitHubSource is the github.workflow_run source: it polls `gh run list`
// through the forge client under the ADR-0039 call budget.
type GitHubSource struct {
	// Provider returns the forge client (nil when there is none). The client
	// must implement forge.WorkflowRunLister.
	Provider func() forge.Provider
	// Workspace resolves a workspace id to its directory.
	Workspace func(id int64) (string, error)
	Ledger    Ledger
	Now       func() time.Time

	mu    sync.Mutex
	floor map[string]time.Time // repo -> last call
	repos map[int64]cachedRepo // workspace -> its repo, for repoCacheTTL
}

type cachedRepo struct {
	repo forge.Repo
	at   time.Time
}

// repoCacheTTL keeps a workspace's repo identity for an hour, so a poll costs
// one forge call (the run list), not two.
const repoCacheTTL = time.Hour

// NewGitHubSource builds the source over st (the ledger and the workspace
// lookup) and a provider accessor.
func NewGitHubSource(st *store.Store, provider func() forge.Provider) *GitHubSource {
	return &GitHubSource{
		Provider: provider,
		Workspace: func(id int64) (string, error) {
			w, err := st.GetWorkspace(id)
			return w.Path, err
		},
		Ledger: st, Now: time.Now,
	}
}

// Kind implements Source.
func (g *GitHubSource) Kind() string { return KindWorkflowRun }

func (g *GitHubSource) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *GitHubSource) lister() (forge.Provider, forge.WorkflowRunLister, error) {
	var p forge.Provider
	if g.Provider != nil {
		p = g.Provider()
	}
	if p == nil {
		return nil, nil, forge.NewError(forge.CodeCLIMissing, "no forge client")
	}
	l, ok := p.(forge.WorkflowRunLister)
	if !ok {
		return nil, nil, fmt.Errorf("forge %s cannot list workflow runs", p.Name())
	}
	return p, l, nil
}

// reserve books one forge call against the automations' own ceiling (60 an
// hour) and the real host's budget (240), and enforces the per-repo floor.
// It returns an *ErrDeferred when a call must wait.
func (g *GitHubSource) reserve(repo forge.Repo, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.floor == nil {
		g.floor = map[string]time.Time{}
	}
	key := repo.Host + "/" + repo.NameWithOwner
	if last, ok := g.floor[key]; ok && now.Sub(last) < repoFloor {
		return &ErrDeferred{Until: last.Add(repoFloor)}
	}
	for _, c := range []struct {
		ledger string
		cap    int
	}{{automationsLedgerPrefix + repo.Host, AutomationHourlyCalls}, {repo.Host, hostHourlyCalls}} {
		calls, start, err := g.Ledger.GetForgeBudget(c.ledger, now)
		if err != nil {
			return fmt.Errorf("read forge budget: %w", err)
		}
		if calls >= c.cap {
			return &ErrDeferred{Until: start.Add(time.Hour)}
		}
	}
	for _, l := range []string{automationsLedgerPrefix + repo.Host, repo.Host} {
		if _, err := g.Ledger.AddForgeCalls(l, 1, now); err != nil {
			return fmt.Errorf("book forge call: %w", err)
		}
	}
	g.floor[key] = now
	return nil
}

func (g *GitHubSource) list(ctx context.Context, a store.Automation, d Def, now time.Time) ([]forge.WorkflowRun, forge.Repo, error) {
	prov, l, err := g.lister()
	if err != nil {
		return nil, forge.Repo{}, err
	}
	dir, err := g.Workspace(a.WorkspaceID)
	if err != nil {
		return nil, forge.Repo{}, fmt.Errorf("workspace %d: %w", a.WorkspaceID, err)
	}
	repo, err := g.repoOf(ctx, prov, a.WorkspaceID, dir, now)
	if err != nil {
		return nil, forge.Repo{}, g.mapErr(err, now)
	}
	if err := g.reserve(repo, now); err != nil {
		return nil, repo, err
	}
	runs, err := l.WorkflowRuns(ctx, dir, forge.WorkflowRunQuery{Workflow: d.Signal.Workflow, Branch: d.Signal.Branch, Limit: pollRunLimit})
	if err != nil {
		return nil, repo, g.mapErr(err, now)
	}
	// Defence in depth: the query names the branch, but a run on any other
	// branch is never a signal, whatever the client returned.
	kept := runs[:0:0]
	for _, r := range runs {
		if r.Branch == d.Signal.Branch {
			kept = append(kept, r)
		}
	}
	return kept, repo, nil
}

func (g *GitHubSource) repoOf(ctx context.Context, prov forge.Provider, ws int64, dir string, now time.Time) (forge.Repo, error) {
	g.mu.Lock()
	if c, ok := g.repos[ws]; ok && now.Sub(c.at) < repoCacheTTL {
		g.mu.Unlock()
		return c.repo, nil
	}
	g.mu.Unlock()
	repo, err := prov.Repo(ctx, dir)
	if err != nil {
		return forge.Repo{}, err
	}
	g.mu.Lock()
	if g.repos == nil {
		g.repos = map[int64]cachedRepo{}
	}
	g.repos[ws] = cachedRepo{repo: repo, at: now}
	g.mu.Unlock()
	return repo, nil
}

// mapErr turns a rate limit into a deferral (no failure counted); everything
// else is a plain error the engine backs off on.
func (g *GitHubSource) mapErr(err error, now time.Time) error {
	var fe *forge.Error
	if errors.As(err, &fe) && fe.Code == forge.CodeRateLimited {
		return &ErrDeferred{Until: now.Add(rateLimitFor)}
	}
	return err
}

func matches(r forge.WorkflowRun, concl []string) bool {
	return r.Status == "completed" && slices.Contains(concl, r.Conclusion)
}

func signalOf(r forge.WorkflowRun, repo forge.Repo) Signal {
	return Signal{
		Kind:        KindWorkflowRun,
		Fingerprint: fmt.Sprintf("gh:%s:%s:%s:%s:%s", repo.NameWithOwner, r.Workflow, r.Branch, r.Conclusion, r.HeadSha),
		Payload: map[string]any{
			"repo": repo.NameWithOwner, "workflow": r.Workflow, "workflowId": r.WorkflowID, "branch": r.Branch,
			"headSha": r.HeadSha, "runId": r.ID, "runUrl": r.URL, "conclusion": r.Conclusion, "event": r.Event,
			"startedAt": r.CreatedAt, "completedAt": r.UpdatedAt,
		},
	}
}

// Poll implements Source. The first poll (empty cursor) primes the cursor to
// the newest run and returns no signal: enabling an automation never replays
// history. Later polls return, oldest first, the completed runs with a listed
// conclusion and an id above the cursor. The returned cursor never passes a
// run that has not completed, so a run that finishes later is still seen.
func (g *GitHubSource) Poll(ctx context.Context, a store.Automation, d Def, now time.Time) (PollResult, error) {
	runs, repo, err := g.list(ctx, a, d, now)
	if err != nil {
		return PollResult{}, err
	}
	var newest int64
	for _, r := range runs {
		newest = max(newest, r.ID)
	}
	if a.PollCursor == "" {
		// Prime below any run still in progress, so one that finishes later
		// is a signal rather than history.
		cursor := newest
		for _, r := range runs {
			if r.Status != "completed" {
				cursor = min(cursor, r.ID-1)
			}
		}
		return PollResult{Cursor: strconv.FormatInt(max(cursor, 0), 10)}, nil
	}
	old, err := strconv.ParseInt(a.PollCursor, 10, 64)
	if err != nil {
		old = newest // a corrupt cursor re-primes instead of replaying
		return PollResult{Cursor: strconv.FormatInt(old, 10)}, nil
	}
	conclusions := d.Signal.Conclusions
	if len(conclusions) == 0 {
		conclusions = []string{"failure"}
	}
	cursor := newest
	var fresh []forge.WorkflowRun
	for _, r := range runs {
		if r.ID <= old {
			continue
		}
		if r.Status != "completed" {
			cursor = min(cursor, r.ID-1)
			continue
		}
		if matches(r, conclusions) {
			fresh = append(fresh, r)
		}
	}
	cursor = max(cursor, old)
	slices.SortFunc(fresh, func(x, y forge.WorkflowRun) int { return int(x.ID - y.ID) })
	out := make([]Signal, 0, len(fresh))
	for _, r := range fresh {
		out = append(out, signalOf(r, repo))
	}
	return PollResult{Signals: out, Cursor: strconv.FormatInt(cursor, 10)}, nil
}

// Latest implements Source: one budgeted call, the newest completed run with a
// listed conclusion (for a human's fire now).
func (g *GitHubSource) Latest(ctx context.Context, a store.Automation, d Def) (Signal, error) {
	now := g.now()
	runs, repo, err := g.list(ctx, a, d, now)
	if err != nil {
		return Signal{}, err
	}
	conclusions := d.Signal.Conclusions
	if len(conclusions) == 0 {
		conclusions = []string{"failure"}
	}
	var best *forge.WorkflowRun
	for i := range runs {
		if matches(runs[i], conclusions) && (best == nil || runs[i].ID > best.ID) {
			best = &runs[i]
		}
	}
	if best == nil {
		return Signal{}, fmt.Errorf("no completed run of %q on %s with conclusion %v", d.Signal.Workflow, d.Signal.Branch, conclusions)
	}
	return signalOf(*best, repo), nil
}
