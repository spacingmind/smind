// Package prevalence is the stage-0 scan of ADR-0037: for a pending learned
// proposal it derives failure signatures from the proposal's evidence and
// counts, over the workspace's recent persisted run events, in how many
// distinct runs and chats each occurred. It makes no model call and writes
// nothing.
package prevalence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/learning/service"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// maxSignatures bounds how many distinct signatures one scan reports.
const maxSignatures = 3

// SignatureCount is one signature's prevalence.
type SignatureCount struct {
	Kind, Tool, Label string
	Runs, Chats       int
	LastSeenAt        *time.Time
	// Truncated says the event read hit the cap, so the counts are lower
	// bounds.
	Truncated bool
}

// Result is the outcome of one scan.
type Result struct {
	WindowDays int
	Signatures []SignatureCount
}

// Deps are the service's collaborators. Only Store is required.
type Deps struct {
	Store *store.Store
	// Config is read at every scan; nil = config.DefaultLearning().
	Config func() config.LearningConfig
	// Home is the smind home dir; its path is redacted from labels.
	Home string
	Now  func() time.Time
	// DecodeEvent decodes run_events.event_data; default runs.DecodeStoredEvent.
	DecodeEvent func(string) (taskrunner.Event, error)
}

// Service runs prevalence scans.
type Service struct{ d Deps }

// New builds a Service.
func New(d Deps) *Service {
	if d.Config == nil {
		d.Config = config.DefaultLearning
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.DecodeEvent == nil {
		d.DecodeEvent = runs.DecodeStoredEvent
	}
	return &Service{d: d}
}

// Scan computes the prevalence of the proposal's evidence signatures. An
// unknown proposal returns an error wrapping sql.ErrNoRows.
func (s *Service) Scan(ctx context.Context, proposalID int64) (Result, error) {
	p, err := s.d.Store.GetProposal(proposalID)
	if err != nil {
		return Result{}, err
	}
	probe := s.d.Config().Probe
	res := Result{WindowDays: probe.PrevalenceDays, Signatures: []SignatureCount{}}

	ws, err := s.d.Store.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return Result{}, err
	}
	rdx := service.NewRedactor(s.d.Store, s.d.Home, ws.Path)

	sigs, err := s.Derive(p, rdx.String)
	if err != nil {
		return Result{}, err
	}
	if len(sigs) == 0 {
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	since := s.d.Now().UTC().Add(-time.Duration(probe.PrevalenceDays) * 24 * time.Hour)
	rows, truncated, err := s.d.Store.ListWorkspaceUserEvents(p.WorkspaceID, since, probe.PrevalenceMaxEvents)
	if err != nil {
		return Result{}, err
	}

	type runData struct {
		chatID  int64
		events  []window.Event
		created map[int64]time.Time // seq -> created_at
	}
	byRun := map[string]*runData{}
	for _, r := range rows {
		ev, err := s.d.DecodeEvent(r.EventData)
		if err != nil {
			slog.Warn("learning prevalence: skip undecodable event", "run", r.RunID, "event", r.ID, "err", err)
			continue
		}
		rd := byRun[r.RunID]
		if rd == nil {
			rd = &runData{chatID: r.ChatID, created: map[int64]time.Time{}}
			byRun[r.RunID] = rd
		}
		rd.events = append(rd.events, window.Event{ID: r.ID, Seq: r.Seq, E: ev})
		rd.created[r.Seq] = r.CreatedAt
	}
	for _, rd := range byRun {
		sort.Slice(rd.events, func(i, j int) bool { return rd.events[i].Seq < rd.events[j].Seq })
	}

	for _, sig := range sigs {
		sc := SignatureCount{Kind: sig.Kind, Tool: sig.Tool, Label: rdx.String(sig.Label()), Truncated: truncated}
		chats := map[int64]bool{}
		for _, rd := range byRun {
			seqs := signature.MatchSeqs(rd.events, sig, rdx.String)
			if len(seqs) == 0 {
				continue
			}
			sc.Runs++
			if rd.chatID != 0 {
				chats[rd.chatID] = true
			}
			for _, seq := range seqs {
				if t, ok := rd.created[seq]; ok && (sc.LastSeenAt == nil || t.After(*sc.LastSeenAt)) {
					t := t.UTC()
					sc.LastSeenAt = &t
				}
			}
		}
		sc.Chats = len(chats)
		res.Signatures = append(res.Signatures, sc)
	}
	return res, nil
}

// Derive returns up to maxSignatures distinct signatures from the
// proposal's evidence. A run is only read when it belongs to the
// proposal's workspace.
func (s *Service) Derive(p store.LearnedProposal, redact func(string) string) ([]signature.Signature, error) {
	var items []gate.Evidence
	if p.Evidence == "" || json.Unmarshal([]byte(p.Evidence), &items) != nil {
		return nil, nil
	}
	var out []signature.Signature
	seenEv := map[gate.Evidence]bool{}
	seenSig := map[string]bool{}
	owned := map[string]bool{}
	events := map[string][]window.Event{}
	for _, ev := range items {
		if ev.Kind != gate.EvFailureRecovery && ev.Kind != gate.EvPermissionDenied {
			continue
		}
		key := gate.Evidence{Kind: ev.Kind, RunID: ev.RunID, Seq: ev.Seq}
		if seenEv[key] {
			continue
		}
		seenEv[key] = true
		if _, ok := owned[ev.RunID]; !ok {
			owned[ev.RunID] = s.runInWorkspace(ev.RunID, p.WorkspaceID)
		}
		if !owned[ev.RunID] {
			continue
		}
		evs, ok := events[ev.RunID]
		if !ok {
			var err error
			if evs, err = s.loadRun(ev.RunID); err != nil {
				return nil, err
			}
			events[ev.RunID] = evs
		}
		sig, ok := signature.DeriveWith(ev, evs, redact)
		if !ok || seenSig[sig.Key()] {
			continue
		}
		seenSig[sig.Key()] = true
		out = append(out, sig)
		if len(out) == maxSignatures {
			break
		}
	}
	return out, nil
}

func (s *Service) runInWorkspace(runID string, workspaceID int64) bool {
	r, err := s.d.Store.GetRun(runID)
	if err != nil {
		return false
	}
	t, err := s.d.Store.GetTask(r.TaskID)
	return err == nil && t.WorkspaceID == workspaceID
}

func (s *Service) loadRun(runID string) ([]window.Event, error) {
	rows, err := s.d.Store.ListRunEvents(runID)
	if err != nil {
		return nil, fmt.Errorf("load run %s events: %w", runID, err)
	}
	out := make([]window.Event, 0, len(rows))
	for _, r := range rows {
		ev, err := s.d.DecodeEvent(r.EventData)
		if err != nil {
			continue
		}
		out = append(out, window.Event{ID: r.ID, Seq: r.Seq, E: ev})
	}
	return out, nil
}
