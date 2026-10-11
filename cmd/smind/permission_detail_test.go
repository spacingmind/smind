package main

import (
	"encoding/json"
	"strings"
	"testing"
)

var testPermOptions = []permissionOptionParams{{ID: "a", Label: "Allow", Kind: "allow_once"}}

func TestRenderPermissionRequest_ShowsDetail(t *testing.T) {
	t.Parallel()
	got := renderPermissionRequest("run-1", "req-1", "Write x.go", testPermOptions, &permissionDetail{
		Kind: "write", Path: "x.go", NewLines: 7, NewBytes: 204, ExistingLines: 240, ExistingBytes: 3174,
		ReplacesExisting: true, OutsideWorktree: true, Redacted: true, Diff: "-old\n+new", Truncated: true,
	})
	for _, want := range []string{
		"  path: x.go\n",
		"  240 -> 7 lines, 3.1 KB -> 0.2 KB\n",
		"  WARNING: replaces an existing file\n",
		"  WARNING: outside the task folder\n",
		"  existing contents hidden (restricted path)\n",
		"    -old\n    +new\n",
		"(truncated)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// header first, then detail, then options
	if strings.Index(got, "[permission]") > strings.Index(got, "path: x.go") || strings.Index(got, "path: x.go") > strings.Index(got, "- a: Allow") {
		t.Errorf("wrong ordering:\n%s", got)
	}

	newFile := renderPermissionRequest("r", "q", "s", nil, &permissionDetail{Kind: "write", Path: "n.go", NewLines: 12, NewBytes: 410, Preview: "hello"})
	if !strings.Contains(newFile, "  new file, 12 lines, 0.4 KB\n") || !strings.Contains(newFile, "    hello\n") {
		t.Errorf("new-file output wrong:\n%s", newFile)
	}

	var lines []string
	for i := 0; i < 45; i++ {
		lines = append(lines, "+l")
	}
	capped := renderPermissionRequest("r", "q", "s", nil, &permissionDetail{Kind: "edit", Diff: strings.Join(lines, "\n")})
	if !strings.Contains(capped, "    ... (5 more lines; see smind task logs)\n") || strings.Count(capped, "    +l\n") != 40 {
		t.Errorf("cap wrong:\n%s", capped)
	}

	exec := renderPermissionRequest("r", "q", "s", nil, &permissionDetail{Kind: "execute", Command: "rm -rf /tmp/x"})
	if !strings.Contains(exec, "  command: rm -rf /tmp/x\n") {
		t.Errorf("execute output wrong:\n%s", exec)
	}
}

func TestRenderPermissionRequest_LargeShrinkWarning(t *testing.T) {
	t.Parallel()
	got := renderPermissionRequest("r", "q", "s", nil, &permissionDetail{
		Kind: "write", Path: "a.go", ExistingLines: 240, NewLines: 7, ReplacesExisting: true, LargeShrink: true,
	})
	want := "  DANGER: overwrites 240 lines with 7 -- most of the file will be lost\n"
	if !strings.Contains(got, want) {
		t.Errorf("missing %q:\n%s", want, got)
	}
}

func TestRenderPermissionRequest_NoDetailUnchanged(t *testing.T) {
	t.Parallel()
	got := renderPermissionRequest("run-1", "req-2", "Bash(ls)", []permissionOptionParams{
		{ID: "a", Label: "Allow", Kind: "allow_once"},
		{ID: "n", Label: "Deny", Kind: "reject_once"},
	}, nil)
	want := "\n[permission] Bash(ls) (request req-2)\n" +
		"  - a: Allow (allow_once)\n" +
		"  - n: Deny (reject_once)\n" +
		"  -> approve: smind task approve run-1 req-2\n" +
		"  -> deny:    smind task deny run-1 req-2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderPermissionRequest_StripsControlCharacters(t *testing.T) {
	t.Parallel()
	got := renderPermissionRequest("r", "q", "s", nil, &permissionDetail{
		Kind: "execute", Path: "a\x1b[2Jb", Command: "ls\r\x07\x1b]0;pwn\x07", Preview: "x\x1b[31mred\x00\u009b\ty\nz",
	})
	for _, r := range got {
		if r != '\n' && r != '\t' && r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control character %q survived:\n%q", r, got)
		}
	}
	if !strings.Contains(got, "x[31mred\ty") || !strings.Contains(got, "path: a[2Jb") {
		t.Errorf("printable text mangled: %q", got)
	}
}

func TestFetchPendingPermissions_CarriesDetail(t *testing.T) {
	t.Parallel()
	raw := `{"runId":"r","status":"running","events":[
		{"type":"permission_request","requestId":"q1","summary":"Write","detail":{"kind":"write","path":"a.go","replacesExisting":true,"largeShrink":true,"diff":"-a\n+b"}}]}`
	var res runLogsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	d := res.Events[0].Detail
	if d == nil || d.Path != "a.go" || !d.ReplacesExisting || !d.LargeShrink || d.Diff != "-a\n+b" {
		t.Fatalf("Detail = %+v", d)
	}
}

// A redacted overwrite carries no existing stats on the wire; the CLI must
// not print the absent zeros as "0 -> N lines".
func TestRenderPermissionRequest_RedactedOverwriteHasNoFakeZeroStats(t *testing.T) {
	out := renderPermissionRequest("r1", "q1", "Write file: .env", nil, &permissionDetail{
		Kind: "write", Path: ".env", NewLines: 3, NewBytes: 40,
		ReplacesExisting: true, Redacted: true,
	})
	if strings.Contains(out, "0 -> 3") || strings.Contains(out, "0.0 KB ->") {
		t.Fatalf("output shows fabricated existing stats:\n%s", out)
	}
	if !strings.Contains(out, "replaces existing file with 3 lines") || !strings.Contains(out, "WARNING: replaces an existing file") {
		t.Fatalf("output lacks the overwrite warning:\n%s", out)
	}
}
