package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"

	"github.com/spacingmind/smind/internal/testutil"
)

type policyFixture struct {
	arm, proj, sibling string
	p                  *Policy
}

func newPolicyFixture(t *testing.T) policyFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := policyFixture{arm: filepath.Join(root, "arm"), proj: filepath.Join(root, "proj"), sibling: filepath.Join(root, "sibling")}
	for _, d := range []string{f.arm, f.proj, f.sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.sibling, "REAL.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.SymlinkOrSkip(t, f.sibling, filepath.Join(f.arm, "link"))
	f.p = &Policy{ArmDir: f.arm, ReadOnlyDirs: []string{f.proj}, AllowBash: func(c string) bool { return c == "allowed-cmd" }}
	return f
}

func TestDecider_AllowsInsideArmOnly(t *testing.T) {
	f := newPolicyFixture(t)
	root := filepath.Dir(f.arm)
	cases := []struct {
		name  string
		tool  string
		input map[string]any
		want  bool
	}{
		{"read inside", "Read", map[string]any{"file_path": filepath.Join(f.arm, "a.txt")}, true},
		{"read relative inside", "Read", map[string]any{"file_path": "a.txt"}, true},
		{"read outside", "Read", map[string]any{"file_path": filepath.Join(f.sibling, "REAL.txt")}, false},
		{"read traversal", "Read", map[string]any{"file_path": "../sibling/REAL.txt"}, false},
		{"read via symlink", "Read", map[string]any{"file_path": filepath.Join(f.arm, "link", "REAL.txt")}, false},
		{"read projection", "Read", map[string]any{"file_path": filepath.Join(f.proj, "s.txt")}, true},
		{"write projection", "Write", map[string]any{"file_path": filepath.Join(f.proj, "s.txt")}, false},
		{"edit projection", "Edit", map[string]any{"file_path": filepath.Join(f.proj, "s.txt")}, false},
		{"write inside", "Write", map[string]any{"file_path": filepath.Join(f.arm, "n.txt")}, true},
		{"write new nested inside", "Write", map[string]any{"file_path": filepath.Join(f.arm, "d", "e", "n.txt")}, true},
		{"edit outside", "Edit", map[string]any{"file_path": filepath.Join(f.sibling, "REAL.txt")}, false},
		{"notebook outside", "NotebookEdit", map[string]any{"notebook_path": filepath.Join(root, "x.ipynb")}, false},
		{"grep no path", "Grep", map[string]any{"pattern": "x"}, true},
		{"grep outside", "Grep", map[string]any{"pattern": "x", "path": f.sibling}, false},
		{"glob absolute pattern", "Glob", map[string]any{"pattern": f.sibling + "/*"}, false},
		{"glob traversal pattern", "Glob", map[string]any{"pattern": "../*"}, false},
		{"skill tool", "Skill", map[string]any{"skill": "smindprobe:learned-a"}, true},
		{"glob inside", "Glob", map[string]any{"pattern": "*.txt"}, true},
	}
	denied := 0
	for _, tc := range cases {
		got, _, _, _, _, err := f.p.Decide(context.Background(), claudecode.CanUseToolRequest{ToolName: tc.tool, Input: tc.input})
		if err != nil || got != tc.want {
			t.Errorf("%s: allow=%v err=%v, want %v", tc.name, got, err, tc.want)
		}
		if !tc.want {
			denied++
		}
	}
	if n := len(f.p.Records()); n != len(cases) {
		t.Errorf("recorded %d requests, want %d", n, len(cases))
	}
	c := f.p.Counts()
	if c.Denied != denied || c.Allowed != len(cases)-denied || f.p.Denials() != denied {
		t.Errorf("counts = %+v, Denials=%d, want %d denied of %d", c, f.p.Denials(), denied, len(cases))
	}
}

func TestDecider_BashOnlyInSandbox(t *testing.T) {
	f := newPolicyFixture(t)
	decide := func(p *Policy, cmd string) bool {
		ok, _, _, _, _, _ := p.Decide(context.Background(), claudecode.CanUseToolRequest{ToolName: "Bash", Input: map[string]any{"command": cmd}})
		return ok
	}
	if decide(f.p, "ls") {
		t.Error("bash allowed without AllowBash approving it")
	}
	if !decide(f.p, "allowed-cmd") {
		t.Error("bash refused although AllowBash approved it")
	}
	def := &Policy{ArmDir: f.arm}
	if decide(def, "ls") || decide(def, "") {
		t.Error("default policy must deny Bash")
	}
	ok, _, msg, _, _, _ := f.p.Decide(context.Background(), claudecode.CanUseToolRequest{ToolName: "WebFetch", Input: map[string]any{"url": "http://x"}})
	if ok || msg == "" {
		t.Errorf("WebFetch allowed=%v msg=%q", ok, msg)
	}
}

func TestDecider_NeverPromptsHuman(t *testing.T) {
	f := newPolicyFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, tool := range []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "NotebookEdit", "MultiEdit", "WebFetch", "WebSearch", "Task", "mcp__x__y", "AskUserQuestion", "ExitPlanMode", "Mystery", ""} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _, _, _, interrupt, err := f.p.Decide(ctx, claudecode.CanUseToolRequest{ToolName: tool, Input: nil})
			if err != nil || interrupt {
				t.Errorf("%q: err=%v interrupt=%v", tool, err, interrupt)
			}
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("Decide(%q) did not return immediately", tool)
		}
	}
	if ctx.Err() != nil {
		t.Error("deciding consumed the context deadline")
	}
}

func TestDecider_NilPolicyDeniesAll(t *testing.T) {
	var p *Policy
	for _, tool := range []string{"Read", "Write", "Bash", "Glob", ""} {
		ok, _, msg, _, _, err := p.Decide(context.Background(), claudecode.CanUseToolRequest{ToolName: tool, Input: map[string]any{"file_path": "x"}})
		if ok || err != nil || msg == "" {
			t.Errorf("%q: allow=%v err=%v msg=%q", tool, ok, err, msg)
		}
	}
	if p.Denials() != 0 || p.Records() != nil {
		t.Error("nil policy must report nothing")
	}
}
