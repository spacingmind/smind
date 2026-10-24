package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitRuleInfo(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.txt")

	push := gitRuleInfo(dir, "git push origin smind/task-x")
	if push == nil || push.Class != "always-ask" || push.Remote != "origin" || push.Branch != "smind/task-x" || !push.PerCall || push.ID != "git-push" {
		t.Fatalf("push = %+v", push)
	}
	if len(push.StagedFiles) != 0 {
		t.Fatalf("push staged = %v", push.StagedFiles)
	}

	commit := gitRuleInfo(dir, "git commit -m x")
	if commit == nil || commit.Class != "commit" || !commit.PerCall || len(commit.StagedFiles) != 1 || commit.StagedFiles[0] != "a.txt" {
		t.Fatalf("commit = %+v", commit)
	}

	un := gitRuleInfo(dir, `eval "git push"`)
	if un == nil || un.Class != "unparsed-git" || un.ID != "unparsed-git" || un.Note != unparsedGitNote {
		t.Fatalf("unparsed = %+v", un)
	}

	for _, c := range []string{"git status", "git log --oneline", "ls -la", "echo git push"} {
		if got := gitRuleInfo(dir, c); got != nil {
			t.Errorf("%q = %+v, want nil", c, got)
		}
	}

	// A non-repo cwd yields no staged files rather than an error.
	if c := gitRuleInfo(t.TempDir(), "git commit -m x"); c == nil || len(c.StagedFiles) != 0 {
		t.Fatalf("non-repo commit = %+v", c)
	}
}

func TestGitRuleInfoStagedCap(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for i := 0; i < gitStagedMaxFiles+5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", ".")
	c := gitRuleInfo(dir, "git commit -m x")
	if c == nil || len(c.StagedFiles) != gitStagedMaxFiles || !c.StagedTruncated {
		t.Fatalf("commit = %+v", c)
	}
}

func TestACPGitRuleDetailOnRequest(t *testing.T) {
	t.Parallel()
	raw := func(cmd string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"toolCallId":"t","title":"Run","kind":"execute","rawInput":{"command":%q}}`, cmd))
	}
	d := BuildPermissionDetail(t.TempDir(), raw("git push origin feat"))
	if d == nil || d.GitRule == nil || d.GitRule.ID != "git-push" || d.GitRule.Remote != "origin" || d.GitRule.Branch != "feat" || !d.GitRule.PerCall {
		t.Fatalf("push detail = %+v", d)
	}
	if d := BuildPermissionDetail(t.TempDir(), raw("git status")); d == nil || d.GitRule != nil {
		t.Fatalf("read-only detail = %+v", d)
	}
	if d := BuildPermissionDetail(t.TempDir(), raw("ls")); d == nil || d.GitRule != nil {
		t.Fatalf("no-git detail = %+v", d)
	}
	b, _ := json.Marshal(BuildPermissionDetail("", raw("git push")))
	if !strings.Contains(string(b), `"gitRule":{`) || !strings.Contains(string(b), `"perCall":true`) {
		t.Fatalf("wire = %s", b)
	}
}

func claudeBashReq(cmd string) claudecode.CanUseToolRequest {
	return claudecode.CanUseToolRequest{ToolName: "Bash", Input: map[string]any{"command": cmd}}
}

func TestClaudeBashGitRuleDetail(t *testing.T) {
	t.Parallel()
	d := &detailRecordingDecider{recordingDecider: recordingDecider{optionID: claudeOptionAllow}}
	if _, _, _, _, _, err := (claudeDeciderAdapter{decider: d, cwd: t.TempDir()}).Decide(context.Background(), claudeBashReq("git push origin x")); err != nil {
		t.Fatal(err)
	}
	if d.gotDetail == nil || d.gotDetail.GitRule == nil || d.gotDetail.GitRule.ID != "git-push" || d.gotDetail.GitRule.Branch != "x" {
		t.Fatalf("detail = %+v", d.gotDetail)
	}
	for _, o := range d.gotOptions {
		if o.Kind != "allow_once" && o.Kind != "reject_once" {
			t.Fatalf("option %+v is not once-only", o)
		}
	}
	d2 := &detailRecordingDecider{recordingDecider: recordingDecider{optionID: claudeOptionAllow}}
	if _, _, _, _, _, err := (claudeDeciderAdapter{decider: d2}).Decide(context.Background(), claudeBashReq("git status")); err != nil {
		t.Fatal(err)
	}
	if d2.gotDetail == nil || d2.gotDetail.GitRule != nil {
		t.Fatalf("read-only detail = %+v", d2.gotDetail)
	}
}

func TestClaudeBashGitRulePipelineForms(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"cd x && git push",
		"git -C d push",
		"sh -c 'git push'",
		"FOO=1 git push",
		"env GIT_X=1 git push origin main",
	} {
		d := claudeBashDetail(t.TempDir(), claudeBashReq(cmd))
		if d == nil || d.GitRule == nil || d.GitRule.Class != "always-ask" {
			t.Errorf("%q: detail = %+v", cmd, d)
		}
	}
}

func TestACPGitRuleStripsAlwaysOptions(t *testing.T) {
	t.Parallel()
	opts := []acp.PermissionOption{
		{OptionID: "aa", Name: "Always allow", Kind: acp.PermissionAllowAlways},
		{OptionID: "ao", Name: "Allow once", Kind: acp.PermissionAllowOnce},
		{OptionID: "ra", Name: "Always reject", Kind: acp.PermissionRejectAlways},
		{OptionID: "ro", Name: "Reject once", Kind: acp.PermissionRejectOnce},
	}
	call := func(cmd string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"toolCallId":"t","title":"Run","kind":"execute","rawInput":{"command":%q}}`, cmd))
	}
	ids := func(o []PermissionOption) []string {
		var out []string
		for _, x := range o {
			out = append(out, x.ID)
		}
		return out
	}

	d := &recordingDecider{optionID: "ao"}
	if _, err := (acpDeciderAdapter{decider: d, cwd: t.TempDir()}).Decide(context.Background(), acp.RequestPermissionParams{ToolCall: call("git push"), Options: opts}); err != nil {
		t.Fatal(err)
	}
	if got := ids(d.gotOptions); !reflect.DeepEqual(got, []string{"ao", "ro"}) {
		t.Fatalf("git push options = %v", got)
	}

	// No git rule: options untouched.
	d = &recordingDecider{optionID: "ao"}
	if _, err := (acpDeciderAdapter{decider: d, cwd: t.TempDir()}).Decide(context.Background(), acp.RequestPermissionParams{ToolCall: call("ls"), Options: opts}); err != nil {
		t.Fatal(err)
	}
	if got := ids(d.gotOptions); !reflect.DeepEqual(got, []string{"aa", "ao", "ra", "ro"}) {
		t.Fatalf("ls options = %v", got)
	}

	// Only standing options offered: keep the original rather than none.
	only := []acp.PermissionOption{{OptionID: "aa", Name: "Always", Kind: acp.PermissionAllowAlways}}
	d = &recordingDecider{optionID: "aa"}
	if _, err := (acpDeciderAdapter{decider: d, cwd: t.TempDir()}).Decide(context.Background(), acp.RequestPermissionParams{ToolCall: call("git push"), Options: only}); err != nil {
		t.Fatal(err)
	}
	if got := ids(d.gotOptions); !reflect.DeepEqual(got, []string{"aa"}) {
		t.Fatalf("only-standing options = %v", got)
	}
}

func TestCodexGitRuleTextOnly(t *testing.T) {
	t.Parallel()
	run := func(cmd string) (string, []PermissionOption) {
		d := &recordingDecider{optionID: codexOptionAccept}
		if _, err := (codexDeciderAdapter{decider: d}).DecideCommandExecution(context.Background(), codex.CommandExecutionApprovalRequest{Command: cmd}); err != nil {
			t.Fatal(err)
		}
		return d.gotSummary, d.gotOptions
	}
	s, opts := run("git push origin x")
	if !strings.HasPrefix(s, "run git push origin x [") || !strings.HasSuffix(s, "]") {
		t.Fatalf("summary = %q", s)
	}
	if len(opts) != 2 {
		t.Fatalf("options = %+v", opts)
	}
	if s, _ := run("git status"); s != "run git status" {
		t.Fatalf("read-only summary = %q", s)
	}
	if s, _ := run("ls"); s != "run ls" {
		t.Fatalf("plain summary = %q", s)
	}
}
