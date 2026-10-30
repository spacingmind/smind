package runs

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestPermissionResolved_PolicyResolutionsEncodeAndDecode proves the policy
// wire additions (ADR-0047 §2.3) survive the run_events round trip: the two
// policy resolutions and their PermissionRuleID, and the Policy footprint on
// a request's PermissionDetail. A row persisted before the change decodes
// with them absent, so no migration is needed.
func TestPermissionResolved_PolicyResolutionsEncodeAndDecode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		resolution taskrunner.PermissionResolution
		ruleID     string
	}{
		{taskrunner.PermissionResolvedByPolicyAllow, "lead-push-own-branch"},
		{taskrunner.PermissionResolvedByPolicyDeny, "no-force-push"},
	} {
		want := Event{
			Type:                 taskrunner.EventTypePermissionResolved,
			PermissionRequestID:  "req-1",
			PermissionOptionID:   "proceed",
			PermissionResolution: tc.resolution,
			PermissionRuleID:     tc.ruleID,
		}
		data, err := encodeEvent(want)
		if err != nil {
			t.Fatalf("encodeEvent(%s) error = %v", tc.resolution, err)
		}
		if !strings.Contains(data, `"permissionRuleId"`) {
			t.Fatalf("encodeEvent(%s) = %s, want a permissionRuleId field", tc.resolution, data)
		}
		got, err := decodeEvent(data)
		if err != nil {
			t.Fatalf("decodeEvent(%s) error = %v", tc.resolution, err)
		}
		if got.PermissionResolution != tc.resolution || got.PermissionRuleID != tc.ruleID ||
			got.PermissionRequestID != "req-1" || got.PermissionOptionID != "proceed" {
			t.Fatalf("round trip of %q = %+v, want rule %q", tc.resolution, got, tc.ruleID)
		}
	}

	// A request the engine escalated carries the policy footprint in its
	// detail; it must round trip like the rest of the detail.
	want := Event{
		Type:                taskrunner.EventTypePermissionRequest,
		PermissionRequestID: "req-2",
		PermissionSummary:   "Bash(git push origin smind/task-x)",
		PermissionDetail: &taskrunner.PermissionDetail{
			Kind:    "execute",
			Command: "git push origin smind/task-x",
			Policy: &taskrunner.PolicyInfo{
				RuleID:     "lead-push-own-branch",
				Why:        "Lead pushes its own task branch",
				Normalized: "git push origin {taskBranch}",
				Action:     "allow",
			},
		},
	}
	data, err := encodeEvent(want)
	if err != nil {
		t.Fatalf("encodeEvent(request) error = %v", err)
	}
	if !strings.Contains(data, `"policy"`) {
		t.Fatalf("encodeEvent(request) = %s, want a detail.policy field", data)
	}
	got, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent(request) error = %v", err)
	}
	if got.PermissionDetail == nil || got.PermissionDetail.Policy == nil {
		t.Fatalf("round trip lost detail.policy: %+v", got.PermissionDetail)
	}
	if p := got.PermissionDetail.Policy; p.RuleID != "lead-push-own-branch" || p.Why != "Lead pushes its own task branch" ||
		p.Normalized != "git push origin {taskBranch}" || p.Action != "allow" {
		t.Fatalf("round trip detail.policy = %+v", p)
	}

	// A row persisted before the fields existed decodes with them empty,
	// not with garbage or an error.
	legacy, err := decodeEvent(`{"type":3,"permissionRequestId":"req-9","permissionOptionId":"deny","permissionResolution":"human"}`)
	if err != nil {
		t.Fatalf("decodeEvent(legacy) error = %v", err)
	}
	if legacy.PermissionRuleID != "" || legacy.PermissionDetail != nil {
		t.Fatalf("legacy row = %+v, want empty rule id and no detail", legacy)
	}
}
