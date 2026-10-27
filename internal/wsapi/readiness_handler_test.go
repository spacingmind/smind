package wsapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spacingmind/smind/internal/readiness"
)

type readinessResp struct {
	WorkspaceID int64               `json:"workspaceId"`
	Findings    []readiness.Finding `json:"findings"`
	CheckedAt   string              `json:"checkedAt"`
}

// TestWorkspaceReadiness_RPC_Guards: the local token connection and a relay
// transport may read it (like workspace.get); a run-scoped agent connection is
// refused; an unknown workspace is an error; the result is bounded.
func TestWorkspaceReadiness_RPC_Guards(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	wid := e.seatTask.WorkspaceID

	resp := callWS(t, e.admin, "workspace.readiness", map[string]any{"workspaceId": wid})
	wantOK(t, resp, "workspace.readiness")
	var got readinessResp
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != wid || len(got.Findings) == 0 || len(got.Findings) > maxReadinessFindings || got.CheckedAt == "" {
		t.Fatalf("result = %+v", got)
	}
	for _, f := range got.Findings {
		if f.ID == "" || f.Status == "" {
			t.Fatalf("incomplete finding %+v", f)
		}
	}

	wantForbidden(t, callWS(t, e.identified(t), "workspace.readiness", map[string]any{"workspaceId": wid}), "not available to agents")

	if resp := callWS(t, e.admin, "workspace.readiness", map[string]any{"workspaceId": 999999}); resp.Error == nil {
		t.Fatal("unknown workspace: want an error")
	}

	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.api.ServeTransport(ctx, server)
	defer client.Close()
	data, _ := json.Marshal(envelope{ID: "r", Method: "workspace.readiness", Params: marshalOrNull(map[string]any{"workspaceId": wid})})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	raw, err := client.Receive()
	if err != nil {
		t.Fatal(err)
	}
	var rel envelope
	if err := json.Unmarshal(raw, &rel); err != nil {
		t.Fatal(err)
	}
	wantOK(t, rel, "workspace.readiness over relay")
}
