package taskrunner

import (
	"context"
	"strings"
	"testing"
)

// reasonDecider implements DenyReasonDecider on top of recordingDecider.
type reasonDecider struct {
	recordingDecider
	reason string
}

func (d *reasonDecider) DecideDetailed(ctx context.Context, summary string, _ *PermissionDetail, options []PermissionOption) (string, error) {
	return d.Decide(ctx, summary, options)
}

func (d *reasonDecider) DecideDetailedWithReason(ctx context.Context, summary string, detail *PermissionDetail, options []PermissionOption) (string, string, error) {
	id, err := d.DecideDetailed(ctx, summary, detail, options)
	return id, d.reason, err
}

func TestClaudeAdapter_PolicyDenyMessageCarriesFloorID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	deny := &reasonDecider{recordingDecider: recordingDecider{optionID: claudeOptionDeny}, reason: "floor no-stash: git stash is denied"}
	for name, req := range map[string]bool{"bash": true, "plain": false} {
		adapter := claudeDeciderAdapter{decider: deny, cwd: t.TempDir()}
		r := claudeBashReq("git stash")
		if !req {
			r.ToolName = "Read"
		}
		allow, _, msg, _, _, err := adapter.Decide(ctx, r)
		if err != nil || allow || !strings.Contains(msg, "no-stash") || !strings.HasPrefix(msg, claudeFixedDenyMessage+": ") {
			t.Fatalf("%s: allow=%v msg=%q err=%v", name, allow, msg, err)
		}
	}

	ok := &reasonDecider{recordingDecider: recordingDecider{optionID: claudeOptionAllow}}
	allow, _, msg, _, _, err := claudeDeciderAdapter{decider: ok, cwd: t.TempDir()}.Decide(ctx, claudeBashReq("git status"))
	if err != nil || !allow || msg != "" {
		t.Fatalf("allow: allow=%v msg=%q err=%v", allow, msg, err)
	}

	noReason := &reasonDecider{recordingDecider: recordingDecider{optionID: claudeOptionDeny}}
	_, _, msg, _, _, _ = claudeDeciderAdapter{decider: noReason}.Decide(ctx, claudeBashReq("ls"))
	if msg != claudeFixedDenyMessage {
		t.Fatalf("empty reason msg = %q", msg)
	}

	plain := &recordingDecider{optionID: claudeOptionDeny}
	_, _, msg, _, _, _ = claudeDeciderAdapter{decider: plain}.Decide(ctx, claudeBashReq("git stash"))
	if msg != claudeFixedDenyMessage {
		t.Fatalf("plain decider msg = %q", msg)
	}
}
