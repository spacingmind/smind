package probesvc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/learning/probe/contrast"
	"github.com/spacingmind/smind/internal/store"
)

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// clipText cuts s to at most n bytes on a rune boundary.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// removeAll deletes a directory the service created, making it writable
// first (a CLI may leave read-only entries).
func removeAll(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

func toContrastView(c contrast.Contrast) ContrastView {
	v := ContrastView{Loaded: c.Loaded, NoiseFloor: c.NoiseFloor, Metrics: make([]MetricRow, 0, len(c.Metrics))}
	for _, r := range c.Metrics {
		v.Metrics = append(v.Metrics, MetricRow{Key: string(r.Key), A: r.A, A2: r.A2, B: r.B, DeltaAB: r.DeltaAB, Noise: r.Noise, Worse: r.Worse})
	}
	return v
}

// Get returns the proposal's latest probe with diffs and final messages.
func (s *Service) Get(proposalID int64) (View, bool, error) {
	p, ok, err := s.d.Store.LatestProbeForProposal(proposalID)
	if err != nil || !ok {
		return View{}, false, err
	}
	v, err := s.view(p, true)
	return v, err == nil, err
}

// List returns the workspace's probes, newest first, without diffs or final
// messages.
func (s *Service) List(workspaceID int64, limit int) ([]View, error) {
	ps, err := s.d.Store.ListProbes(workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(ps))
	for _, p := range ps {
		v, err := s.view(p, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) view(p store.LearningProbe, full bool) (View, error) {
	arms, err := s.d.Store.ListProbeArms(p.ID)
	if err != nil {
		return View{}, err
	}
	now := s.d.Now()
	v := View{ID: p.ID, ProposalID: p.ProposalID, WorkspaceID: p.WorkspaceID, Quick: p.Quick, Status: p.Status,
		VerdictReason: p.VerdictReason, EstUSD: p.EstUSD, CostUSD: p.CanaryCostUSD,
		StartedAt: p.StartedAt.UTC().Format(time.RFC3339), Arms: make([]ArmView, 0, len(arms))}
	if p.Verdict != "" {
		verdict := p.Verdict
		v.Verdict = &verdict
	}
	end := now
	if p.FinishedAt != nil {
		v.FinishedAt = p.FinishedAt.UTC().Format(time.RFC3339)
		end = *p.FinishedAt
	}
	v.ElapsedMs = max(0, end.Sub(p.StartedAt).Milliseconds())
	for _, a := range arms {
		v.CostUSD += a.CostUSD
		av := ArmView{Role: a.Role, Status: a.Status, RunID: a.RunID, CostUSD: a.CostUSD}
		var m armMetrics
		_ = json.Unmarshal([]byte(a.Metrics), &m)
		av.Turns, av.ToolCalls, av.FailedToolCalls, av.PermissionDenials = m.Turns, m.ToolCalls, m.FailedToolCalls, m.PermissionDenials
		av.LoadedSkill, av.FailureRecurred = m.LoadedSkill, m.FailureRecurred
		av.DiffBytes, av.ProducedDiff = m.DiffBytes, m.DiffBytes > 0
		if a.StartedAt != nil {
			av.StartedAt = a.StartedAt.UTC().Format(time.RFC3339)
			until := now
			if a.FinishedAt != nil {
				until = *a.FinishedAt
			}
			av.ElapsedMs = max(0, until.Sub(*a.StartedAt).Milliseconds())
		}
		if a.CheckResult != "" {
			var c Check
			if json.Unmarshal([]byte(a.CheckResult), &c) == nil {
				av.Check = &c
			}
		}
		if full {
			av.Diff, av.FinalMessage = a.Diff, a.FinalMessage
		}
		v.Arms = append(v.Arms, av)
	}
	if p.Status == statusDone && strings.TrimSpace(p.Contrast) != "" && p.Contrast != "{}" {
		var c ContrastView
		if json.Unmarshal([]byte(p.Contrast), &c) == nil && len(c.Metrics) > 0 {
			v.Contrast = &c
		}
	}
	return v, nil
}
