package acp

import (
	"context"
	"encoding/json"
	"testing"
)

var permOptions = []PermissionOption{
	{OptionID: "allow-1", Name: "Allow", Kind: PermissionAllowOnce},
	{OptionID: "allow-always-1", Name: "Allow Always", Kind: PermissionAllowAlways},
	{OptionID: "reject-1", Name: "Reject", Kind: PermissionRejectOnce},
	{OptionID: "reject-always-1", Name: "Reject Always", Kind: PermissionRejectAlways},
}

func TestAutoApprovePolicy_Decide(t *testing.T) {
	t.Parallel()
	got, err := AutoApprovePolicy{}.Decide(context.Background(), RequestPermissionParams{Options: permOptions})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got != "allow-1" {
		t.Fatalf("Decide() = %q, want %q", got, "allow-1")
	}
}

func TestAutoApprovePolicy_Decide_NoAllowOption(t *testing.T) {
	t.Parallel()
	options := []PermissionOption{{OptionID: "reject-1", Name: "Reject", Kind: PermissionRejectOnce}}
	if _, err := (AutoApprovePolicy{}).Decide(context.Background(), RequestPermissionParams{Options: options}); err == nil {
		t.Fatal("Decide() succeeded, want error when no allow option is offered")
	}
}

func TestAutoDenyPolicy_Decide(t *testing.T) {
	t.Parallel()
	got, err := AutoDenyPolicy{}.Decide(context.Background(), RequestPermissionParams{Options: permOptions})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got != "reject-1" {
		t.Fatalf("Decide() = %q, want %q", got, "reject-1")
	}
}

func TestAutoDenyPolicy_Decide_NoRejectOption(t *testing.T) {
	t.Parallel()
	options := []PermissionOption{{OptionID: "allow-1", Name: "Allow", Kind: PermissionAllowOnce}}
	if _, err := (AutoDenyPolicy{}).Decide(context.Background(), RequestPermissionParams{Options: options}); err == nil {
		t.Fatal("Decide() succeeded, want error when no reject option is offered")
	}
}

func TestClient_HandleRequestPermission(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(RequestPermissionParams{SessionID: "s1", Options: permOptions})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	tests := []struct {
		name       string
		policy     PermissionPolicy
		wantOption string
	}{
		{"auto approve", AutoApprovePolicy{}, "allow-1"},
		{"auto deny", AutoDenyPolicy{}, "reject-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{policy: tt.policy}
			result, rpcErr := c.handleRequestPermission(context.Background(), raw)
			if rpcErr != nil {
				t.Fatalf("handleRequestPermission() error = %v", rpcErr)
			}
			outcome := result.(requestPermissionResult).Outcome
			if outcome.Outcome != "selected" || outcome.OptionID != tt.wantOption {
				t.Fatalf("handleRequestPermission() outcome = %+v, want optionId %q", outcome, tt.wantOption)
			}
		})
	}
}
