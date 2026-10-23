package forge

import (
	"errors"
	"strings"
	"testing"
)

func TestPRError_FixedSentencesAndWire(t *testing.T) {
	cases := map[string]string{
		CodePushRejected: "GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first.",
		CodePushAuth:     "Git couldn't sign in to push. Check the daemon host's git credentials, or run `gh auth login` there.",
	}
	for code, want := range cases {
		e := NewPRError(code, "")
		if e.Error() != want || e.RPCCode() != code || e.RPCData() != nil {
			t.Fatalf("%s: %q / %q / %v", code, e.Error(), e.RPCCode(), e.RPCData())
		}
	}
	if got := CherryPickConflict("develop", "").Error(); got != "smind couldn't replay your commits onto `develop` without conflicts. Update this branch from `develop` first." {
		t.Fatalf("cherry-pick message = %q", got)
	}
	if got := UpdateUnsupported("smind/pr-task-12").Error(); got != "This pull request was opened from a rebased copy (`smind/pr-task-12`), so smind can't update it automatically." {
		t.Fatalf("update_unsupported message = %q", got)
	}
	for _, code := range []string{CodeTaskNoWorktree, CodeBaseUnknown, CodeBaseNotFound, CodeRunActive, CodeNoCommits, CodePRExists, CodePRNotOpen, CodeNoPR,
		CodeForkPRReadonly, CodeInvalidTitle, CodeInvalidBody, CodeInvalidBranch, CodePRBusy, CodeTimeout, CodeError, CodeNoRemote} {
		if e := NewPRError(code, ""); e.Message == "" || e.Message == "The pull request action failed." {
			t.Fatalf("%s has no standard sentence", code)
		}
	}
}

func TestPRError_DetailScrubbedClippedAndDataCarried(t *testing.T) {
	e := NewPRError(CodePushAuth, "fatal: https://u:ghp_abcdefghijklmnopqrstuvwxyz0123456789@github.com "+strings.Repeat("x", 500)).
		With("url", "https://github.com/o/r/pull/1").With("number", 1)
	d, _ := e.Data["detail"].(string)
	if strings.Contains(d, "ghp_") || len(d) > detailClip+len("…") {
		t.Fatalf("detail not scrubbed/clipped: %q", d)
	}
	if data, ok := e.RPCData().(map[string]any); !ok || data["url"] != "https://github.com/o/r/pull/1" || data["number"] != 1 {
		t.Fatalf("data = %v", e.RPCData())
	}
	var pe *PRError
	if !errors.As(error(e), &pe) {
		t.Fatal("not an error value")
	}
}
