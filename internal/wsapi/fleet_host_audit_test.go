package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/serve"
	"github.com/spacingmind/smind/internal/store"
)

func (e *fleetEnv) auditCall(t *testing.T, rc *requestContext, params any) (fleetHostAuditResult, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := e.api.hs["fleet.host.audit"](context.Background(), rc, raw)
	if err != nil {
		return fleetHostAuditResult{}, err
	}
	return out.(fleetHostAuditResult), nil
}

func (e *fleetEnv) seedAudit(t *testing.T, rows ...store.FleetAuditRow) {
	t.Helper()
	for i, r := range rows {
		if r.TS.IsZero() {
			r.TS = time.Date(2026, 11, 1, 10, i, 0, 0, time.UTC)
		}
		if _, err := e.st.AppendFleetAudit(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFleetHostAudit_ViewShape(t *testing.T) {
	e := newFleetEnv(t)
	caller := newCallerKey(t)
	e.pairApproved(t, caller, "peer")
	other := newCallerKey(t)
	e.pairApproved(t, other, "other")
	e.seedAudit(t,
		store.FleetAuditRow{Direction: "in", HostID: caller.HostID(), Trace: "tr-secret", ReqID: "req-secret", Method: "fleet.task.list", Outcome: "ok", LatencyMs: 4, Summary: "sha256=ab {}"},
		store.FleetAuditRow{Direction: "in", HostID: other.HostID(), Method: "fleet.task.get", Outcome: "refused", Code: "not_granted", Summary: "sha256=cd {}"},
		store.FleetAuditRow{Direction: "out", HostID: "0123456789abcdef-removed", Method: "fleet.info", Outcome: "ok", Summary: "sha256=ef {}"},
	)
	rc := &requestContext{local: true}

	all, err := e.auditCall(t, rc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(all.Rows))
	}
	if all.Rows[0].Method != "fleet.info" || all.Rows[2].Method != "fleet.task.list" {
		t.Fatalf("not newest first: %+v", all.Rows)
	}
	if got := all.Rows[0].Peer; got != "01234567" {
		t.Errorf("removed host peer = %q, want its short id", got)
	}
	r := all.Rows[1]
	if r.Direction != "in" || r.Peer != "other" || r.Decision != "refused" || r.Code != "not_granted" || r.Time.IsZero() {
		t.Errorf("row = %+v", r)
	}

	only, err := e.auditCall(t, rc, map[string]any{"host": "peer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(only.Rows) != 1 || only.Rows[0].Peer != "peer" || only.Rows[0].LatencyMs != 4 {
		t.Fatalf("host filter: %+v", only.Rows)
	}
	byID, err := e.auditCall(t, rc, map[string]any{"hostId": caller.HostID()})
	if err != nil || len(byID.Rows) != 1 {
		t.Fatalf("hostId filter: %v %+v", err, byID.Rows)
	}

	one, err := e.auditCall(t, rc, map[string]any{"limit": 1})
	if err != nil || len(one.Rows) != 1 {
		t.Fatalf("limit: %v %+v", err, one.Rows)
	}

	// No trace or request id leaves the daemon in this view.
	raw, _ := json.Marshal(all)
	for _, leak := range []string{"tr-secret", "req-secret", "trace", "reqId"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("view carries %q: %s", leak, raw)
		}
	}

	var ce *rpcCodeError
	if _, err := e.auditCall(t, rc, map[string]any{"host": "nobody"}); !errors.As(err, &ce) || ce.code != codeNotFound {
		t.Errorf("unknown host: %v", err)
	}
	if _, err := e.auditCall(t, rc, map[string]any{"limit": -1}); !errors.As(err, &ce) || ce.code != codeInvalid {
		t.Errorf("negative limit: %v", err)
	}
}

func TestFleetHostAudit_LimitIsClamped(t *testing.T) {
	e := newFleetEnv(t)
	for i := 0; i < fleetAuditMaxLimit+20; i++ {
		e.seedAudit(t, store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.info", Outcome: "ok"})
	}
	rc := &requestContext{local: true}
	def, _ := e.auditCall(t, rc, map[string]any{})
	if len(def.Rows) != fleetAuditDefaultLimit {
		t.Errorf("default limit gave %d rows", len(def.Rows))
	}
	big, _ := e.auditCall(t, rc, map[string]any{"limit": 100000})
	if len(big.Rows) != fleetAuditMaxLimit {
		t.Errorf("huge limit gave %d rows, want %d", len(big.Rows), fleetAuditMaxLimit)
	}
}

// TestFleetHostAudit_NeverShowsPromptText: whatever a row holds, the view shows no
// prompt. Rows written by the current writer carry only a hash and a masked prefix;
// a row from before the masking (or a writer slip) is masked again on the way out,
// including a value cut off by the 120-character prefix and one with escaped quotes.
func TestFleetHostAudit_NeverShowsPromptText(t *testing.T) {
	e := newFleetEnv(t)
	const secret = "migrate the billing tables then email the CEO"
	body := json.RawMessage(`{"prov":{"reqId":"r1"},"workspaceId":3,"prompt":"` + secret + `","title":"t"}`)
	current := serve.Summary(body, func(s string) string { return s })
	legacyFull := `sha256=00 {"prompt":"` + secret + ` \"quoted\" tail","workspaceId":3}`
	legacyCut := `sha256=00 {"workspaceId":3,"message":"` + secret[:20]
	legacyNested := `sha256=00 {"a":{"content":"` + secret + `"}}`
	e.seedAudit(t,
		store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.task.create", Outcome: "ok", Summary: current},
		store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.task.send", Outcome: "ok", Summary: legacyFull},
		store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.task.send", Outcome: "ok", Summary: legacyCut},
		store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.task.send", Outcome: "ok", Summary: legacyNested},
	)
	res, err := e.auditCall(t, &requestContext{local: true}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 4 {
		t.Fatalf("rows = %d", len(res.Rows))
	}
	for _, r := range res.Rows {
		for _, leak := range []string{"migrate", "billing", "CEO", "quoted", "tail"} {
			if strings.Contains(r.Summary, leak) {
				t.Errorf("%s summary leaks %q: %s", r.Method, leak, r.Summary)
			}
		}
		if !strings.HasPrefix(r.Summary, "sha256=") {
			t.Errorf("summary lost its hash: %s", r.Summary)
		}
	}
	// What is not free text stays readable.
	if !strings.Contains(res.Rows[3].Summary, `"workspaceId":3`) {
		t.Errorf("non-text field masked: %s", res.Rows[3].Summary)
	}
	// A value the writer already masked keeps its length marker; a legacy one gets an ellipsis.
	if !strings.Contains(res.Rows[3].Summary, `"prompt":"<45 chars>"`) {
		t.Errorf("writer's marker not kept: %s", res.Rows[3].Summary)
	}
	if !strings.Contains(res.Rows[2].Summary, `"prompt":"…"`) {
		t.Errorf("legacy value not masked: %s", res.Rows[2].Summary)
	}
}

// TestFleetHostAudit_HumanOnly: the relay, an agent, a plugin and a peer host are all
// refused, and a refused call reads nothing.
func TestFleetHostAudit_HumanOnly(t *testing.T) {
	e := newFleetEnv(t)
	e.seedAudit(t, store.FleetAuditRow{Direction: "in", HostID: "h", Method: "fleet.info", Outcome: "ok", Summary: "sha256=00 {}"})

	var ce *rpcCodeError
	if _, err := e.auditCall(t, &requestContext{local: false}, map[string]any{}); !errors.As(err, &ce) || ce.code != codeForbidden {
		t.Errorf("relay: error = %v, want %s", err, codeForbidden)
	}
	if err := (&conn{runScoped: true}).gate("fleet.host.audit"); !errors.As(err, &ce) || ce.code != codeForbidden {
		t.Errorf("agent (run-scoped): error = %v, want %s", err, codeForbidden)
	}
	if runScopedSet["fleet.host.audit"] {
		t.Error("fleet.host.audit is run-scoped")
	}
	if _, ok := pluginMethodCap["fleet.host.audit"]; ok {
		t.Error("fleet.host.audit is in the plugin capability table")
	}
	for kind, methods := range fleetHostAllowed {
		if slices.Contains(methods, "fleet.host.audit") {
			t.Errorf("host kind %q may call fleet.host.audit", kind)
		}
	}
	for _, kind := range []string{"pairing", "pending", "approved"} {
		c := &conn{host: &hostPrincipal{kind: kind, hostID: "h"}}
		if err := c.gate("fleet.host.audit"); !errors.As(err, &ce) || ce.code != codeForbidden {
			t.Errorf("%s host: error = %v, want %s", kind, err, codeForbidden)
		}
	}
}
