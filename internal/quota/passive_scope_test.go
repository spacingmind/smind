package quota

import (
	"net/http"
	"testing"
	"time"
)

// Test_passive_cooldown_scope_by_response_shape pins, for each 429 header
// shape, whether Observe cools down the whole account, only the rejected
// model, or nothing: ClassifyRejection plus unifiedStatuses.modelOnly decide,
// and the row must land in the store exactly once.
func Test_passive_cooldown_scope_by_response_shape(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		hdr  func(now time.Time) http.Header
		// wantScope is one of "account", "model", "none".
		wantScope string
	}{
		{
			name: "5h rejected",
			hdr: func(now time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "rejected",
					uf+"5h-reset", epoch(now.Add(time.Hour)))
			},
			wantScope: "account",
		},
		{
			name: "7d rejected",
			hdr: func(now time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "rejected")
			},
			wantScope: "account",
		},
		{
			name: "strict fable-only (allowed,allowed,oi rejected)",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
					uf+"7d_oi-status", "rejected")
			},
			wantScope: "model",
		},
		{
			name: "fable-only with allowed_warning shared windows",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed_warning", uf+"7d-status", "allowed_warning",
					uf+"7d_oi-status", "rejected")
			},
			wantScope: "model",
		},
		{
			name: "fable-only, 5h status omitted, no 5h utilization",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"7d-status", "allowed", uf+"7d_oi-status", "rejected")
			},
			wantScope: "model",
		},
		{
			name: "fable-only, 5h omitted, healthy 5h utilization 0.4, unified rejected",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"7d-status", "allowed", uf+"7d_oi-status", "rejected",
					uf+"5h-utilization", "0.4")
			},
			wantScope: "model",
		},
		{
			name: "fable-only, 7d omitted, healthy 7d utilization",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d_oi-status", "rejected",
					uf+"7d-utilization", "0.3")
			},
			wantScope: "model",
		},
		{
			name: "overage status rejected, windows allowed",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
					uf+"overage-status", "rejected")
			},
			wantScope: "model",
		},
		{
			name: "overage disabled reason, windows allowed",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
					uf+"overage-disabled-reason", "out_of_credits")
			},
			wantScope: "model",
		},
		{
			name: "representative claim overage, both windows omitted, utilization absent",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"representative-claim", "overage")
			},
			wantScope: "model",
		},
		{
			name: "claim overage, both windows omitted, 5h utilization 1.0 (exhausted)",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"representative-claim", "overage",
					uf+"5h-utilization", "1.0")
			},
			wantScope: "account",
		},
		{
			name: "fable-only but 5h utilization exhausted with 5h omitted",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"7d-status", "allowed", uf+"7d_oi-status", "rejected",
					uf+"5h-utilization", "1.0")
			},
			wantScope: "account",
		},
		{
			name: "fable-only but 7d omitted and no utilization",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d_oi-status", "rejected")
			},
			wantScope: "account",
		},
		{
			name: "oi rejected but 7d rejected too",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "rejected",
					uf+"7d_oi-status", "rejected")
			},
			wantScope: "account",
		},
		{
			name: "unified rejected with no detail",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected")
			},
			wantScope: "account",
		},
		{
			name: "overage claim but 5h rejected",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "rejected", uf+"5h-status", "rejected", uf+"representative-claim", "overage")
			},
			wantScope: "account",
		},
		{
			name: "oi-only without unified rejected",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"5h-status", "allowed", uf+"7d-status", "allowed", uf+"7d_oi-status", "rejected")
			},
			wantScope: "none",
		},
		{
			name:      "plain 429, no headers",
			hdr:       func(time.Time) http.Header { return nil },
			wantScope: "none",
		},
		{
			name: "429 allowed windows",
			hdr: func(time.Time) http.Header {
				return hdr(uf+"status", "allowed", uf+"5h-status", "allowed", uf+"7d-status", "allowed")
			},
			wantScope: "none",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			fs := newFakeStore()
			p, clk := newFakePassive(t, fs)
			now := clk.Now()
			p.Observe(respAt(7, "claude-opus-5", 429, row.hdr(now), now))
			p.Close()
			cs := p.Active(7, now)
			switch row.wantScope {
			case "account":
				if len(cs) != 1 || cs[0].Scope != CooldownAccount {
					t.Fatalf("Active() = %+v, want exactly 1 account cooldown", cs)
				}
				if n := fs.rowCount(); n != 1 {
					t.Errorf("rowCount = %d, want 1", n)
				}
			case "model":
				if len(cs) != 1 || cs[0].Scope != CooldownModel || cs[0].Model != "claude-opus-5" {
					t.Fatalf("Active() = %+v, want exactly 1 model cooldown for claude-opus-5", cs)
				}
				wantNoCooldown(t, p, 7, "some-other-model", now)
				if n := fs.rowCount(); n != 1 {
					t.Errorf("rowCount = %d, want 1", n)
				}
			case "none":
				if len(cs) != 0 {
					t.Fatalf("Active() = %+v, want none", cs)
				}
				if n := fs.rowCount(); n != 0 {
					t.Errorf("rowCount = %d, want 0", n)
				}
			}
		})
	}
}

// Test_passive_model_scoped_rejection_unnamed_model_cools_nothing: a
// model-only rejection whose request named no model cools nothing and is only
// counted.
func Test_passive_model_scoped_rejection_unnamed_model_cools_nothing(t *testing.T) {
	t.Parallel()
	shapes := []http.Header{
		hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
			uf+"7d_oi-status", "rejected"),
		hdr(uf+"status", "rejected", uf+"7d-status", "allowed", uf+"7d_oi-status", "rejected"),
		hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
			uf+"overage-status", "rejected"),
	}
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	for _, h := range shapes {
		p.Observe(respAt(7, "", 429, h, now))
	}
	if cs := p.Active(7, now); len(cs) != 0 {
		t.Errorf("Active() = %+v, want none", cs)
	}
	p.Close()
	if n := fs.rowCount(); n != 0 {
		t.Errorf("rowCount = %d, want 0", n)
	}
	if n := p.unnamedModelOnly.Load(); n != 3 {
		t.Errorf("unnamed model-only rejections counted = %d, want 3", n)
	}
}
