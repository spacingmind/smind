package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	probesvc "github.com/spacingmind/smind/internal/learning/probe/service"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// fakeProbe answers every learning.probe.* call from canned JSON set by the
// test (learnProbeJSON); it counts Start calls and never touches a model.
type fakeProbe struct {
	started int
}

func (f *fakeProbe) Estimate(context.Context, int64, bool) (probesvc.Estimate, error) {
	var est probesvc.Estimate
	if err := json.Unmarshal([]byte(learnProbeJSON), &est); err != nil {
		return probesvc.Estimate{}, err
	}
	return est, nil
}

func (f *fakeProbe) Start(_ context.Context, _ int64, _ bool) (store.LearningProbe, error) {
	f.started++
	var reasons []probesvc.Reason
	if learnProbeJSON != "" && strings.Contains(learnProbeJSON, "learning_off") {
		reasons = append(reasons, probesvc.Reason{Code: probesvc.ReasonLearningOff, Message: "Learning is switched off."})
		return store.LearningProbe{}, &probesvc.IneligibleError{Reasons: reasons}
	}
	return store.LearningProbe{ID: 9, ProposalID: 1, Status: "queued"}, nil
}

func (f *fakeProbe) Cancel(int64) error { return nil }

func (f *fakeProbe) Get(_ int64) (probesvc.View, bool, error) {
	if learnProbeJSON == "null" {
		return probesvc.View{}, false, nil
	}
	var v probesvc.View
	if err := json.Unmarshal([]byte(learnProbeJSON), &v); err != nil {
		return probesvc.View{}, false, err
	}
	return v, true, nil
}

func (f *fakeProbe) List(int64, int) ([]probesvc.View, error) { return []probesvc.View{}, nil }

// learnProbeJSON is the canned learning.probe.* answer each test sets.
var learnProbeJSON string

// newLearnProbeFx starts a daemon whose learning.probe.* methods answer from
// the fake probe probesvc.
func newLearnProbeFx(t *testing.T) *fakeProbe {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatal(err)
	}
	api, err := wsapi.New(workspace.New(s), accounts.New(s), nil, s, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	fp := &fakeProbe{}
	api.SetLearning(wsapi.LearningDeps{Probe: fp})
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: "+u.Port()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return fp
}

// TestLearnProbe_DryRunAndStart: --dry-run calls learning.probe.estimate and
// prints the eligible/reasons/estimate lines; a start prints the started
// banner with the follow-up hint.
func TestLearnProbe_DryRunAndStart(t *testing.T) {
	fp := newLearnProbeFx(t)
	learnProbeJSON = `{"eligible":false,"reasons":[{"code":"learning_off","message":"Learning is switched off."}],"quick":false,"arms":3,"estUSD":1.8,"estArmUSD":0.6,"expectedMinutes":12,"perProbeCapUSD":1.5,"dailyCapUSD":3,"spentTodayUSD":0,"quotaPercent":null}`
	code, out, errOut := learn(t, "probe", "1", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run = %d (%s)", code, errOut)
	}
	for _, part := range []string{"eligible: no", "reason: learning_off: Learning is switched off.",
		"estimate: USD 1.8000 for 3 arms (~12 min); cap USD 1.50; spent today USD 0.0000"} {
		if !strings.Contains(out, part) {
			t.Fatalf("dry-run output %q missing %q", out, part)
		}
	}
	if fp.started != 0 {
		t.Fatalf("dry-run made %d start call(s)", fp.started)
	}

	learnProbeJSON = `{"id":9,"proposalId":1,"workspaceId":1,"quick":false,"status":"queued","verdict":null,"verdictReason":"","estUSD":1.8,"costUSD":0,"startedAt":"2026-10-21T10:00:00Z","elapsedMs":0,"arms":[]}`
	code, out, errOut = learn(t, "probe", "1")
	if code != 0 {
		t.Fatalf("start = %d (%s)", code, errOut)
	}
	if !strings.Contains(out, "probe 9 started for proposal 1") || !strings.Contains(out, "smind learn probe show 1") {
		t.Fatalf("start output = %q", out)
	}
}

// TestLearnProbe_ShowRendersContrast: show prints status, verdict, one line
// per arm and the aligned contrast table with the disclaimer.
func TestLearnProbe_ShowRendersContrast(t *testing.T) {
	newLearnProbeFx(t)
	learnProbeJSON = `{"id":9,"proposalId":1,"workspaceId":1,"quick":false,"status":"done","verdict":"inconclusive","verdictReason":"one replay cannot decide.","estUSD":1.8,"costUSD":1.2,"startedAt":"2026-10-21T10:00:00Z","finishedAt":"2026-10-21T10:12:00Z","elapsedMs":720000,"arms":[{"role":"A","status":"done","runId":"ra","elapsedMs":72000,"costUSD":0.41,"loadedSkill":null,"failureRecurred":false,"producedDiff":true,"diffBytes":120},{"role":"A2","status":"done","runId":"rb","elapsedMs":70000,"costUSD":0.39,"failureRecurred":null,"producedDiff":false,"diffBytes":0},{"role":"B","status":"done","runId":"rc","elapsedMs":80000,"costUSD":0.4,"loadedSkill":true,"failureRecurred":true,"producedDiff":true,"diffBytes":300}],"contrast":{"loaded":true,"noiseFloor":true,"metrics":[{"key":"toolCalls","a":10,"a2":11,"b":9,"deltaAB":-1,"noise":2,"worse":false}]}}`
	code, out, errOut := learn(t, "probe", "show", "1")
	if code != 0 {
		t.Fatalf("show = %d (%s)", code, errOut)
	}
	for _, part := range []string{
		"probe 9 for proposal 1: done",
		"verdict: inconclusive (one replay cannot decide.)",
		"A  done  1m12s  $0.4100  loaded=- recurred=no",
		"B  done  1m20s  $0.4000  loaded=yes recurred=yes",
		"METRIC", "CONTROL", "WITH SKILL",
		"toolCalls",
		"Behavioural contrast on one replay of the source episode, not a measurement.",
	} {
		if !strings.Contains(out, part) {
			t.Fatalf("show output missing %q:\n%s", part, out)
		}
	}

	// --json prints the raw result.
	code, out, _ = learn(t, "probe", "show", "1", "--json")
	if code != 0 || !strings.Contains(out, `"proposalId": 1`) {
		t.Fatalf("show --json = %d %q", code, out)
	}

	// No probe yet: get answers null.
	learnProbeJSON = `null`
	code, out, _ = learn(t, "probe", "show", "1")
	if code != 0 || out != "no probe for proposal 1\n" {
		t.Fatalf("show null = %d %q", code, out)
	}
}

// TestLearnProbe_IneligibleExitsNonZero: a failed_precondition start error
// exits non-zero with the reason message.
func TestLearnProbe_IneligibleExitsNonZero(t *testing.T) {
	newLearnProbeFx(t)
	learnProbeJSON = `{"eligible":false,"reasons":[{"code":"learning_off","message":"Learning is switched off."}]}`
	code, _, errOut := learn(t, "probe", "1")
	if code != 1 || !strings.Contains(errOut, "Learning is switched off.") {
		t.Fatalf("start ineligible = %d %q", code, errOut)
	}
}

// TestLearnProbe_UsageErrors: bad invocations exit 2.
func TestLearnProbe_UsageErrors(t *testing.T) {
	newLearnProbeFx(t)
	for _, args := range [][]string{
		{"probe"},
		{"probe", "abc"},
		{"probe", "1", "--bogus"},
		{"probe", "show"},
		{"probe", "show", "0"},
	} {
		if code, _, _ := learn(t, args...); code != 2 {
			t.Fatalf("learn %v = %d, want 2", args, code)
		}
	}
}
