package forge

import (
	"strings"
	"testing"
	"time"
)

func TestError_MessageIsFixedSentence(t *testing.T) {
	e := NewError(CodeUnauthenticated, "gh pr list --search secret ghp_"+strings.Repeat("a", 30))
	if e.Error() != msgUnauthenticated || e.RPCCode() != CodeUnauthenticated {
		t.Fatalf("Error() = %q code %q", e.Error(), e.RPCCode())
	}
	if !strings.Contains(e.Error(), "gh auth login") {
		t.Fatalf("message should name the next step: %q", e.Error())
	}
	if NewError("bogus", "x").Code != CodeError {
		t.Fatal("unknown code should become forge_error")
	}
}

func TestError_RPCDataDetailAndRetryAt(t *testing.T) {
	if d := (&Error{Code: CodeError}).RPCData(); d != nil {
		t.Fatalf("RPCData() = %v, want nil", d)
	}
	m := (&Error{Detail: "boom"}).RPCData().(map[string]any)
	if m["detail"] != "boom" || len(m) != 1 {
		t.Fatalf("detail only = %v", m)
	}
	at := time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC)
	m = (&Error{RetryAt: at}).RPCData().(map[string]any)
	if m["retryAt"] != "2026-10-22T01:02:03Z" || len(m) != 1 {
		t.Fatalf("retryAt only = %v", m)
	}
}

func TestScrub_Table(t *testing.T) {
	tok := "ghp_" + strings.Repeat("A", 30)
	cases := []struct{ name, in, want string }{
		{"classic", "bad " + tok + " end", "bad [redacted] end"},
		{"oauth", "gho_" + strings.Repeat("b", 25), "[redacted]"},
		{"fine-grained", "github_pat_" + strings.Repeat("C_", 15), "[redacted]"},
		{"auth header", "Authorization: token abc\nnext", "Authorization: [redacted]\nnext"},
		{"userinfo", "fatal: https://user:pw@github.com/o/r", "fatal: https://[redacted]@github.com/o/r"},
		{"x-access-token", "https://x-access-token:tok123@github.com/o/r", "https://[redacted]@github.com/o/r"},
		{"clean", "gh: HTTP 404: Not Found (https://api.github.com/repos/o/r)", "gh: HTTP 404: Not Found (https://api.github.com/repos/o/r)"},
	}
	for _, tc := range cases {
		if got := Scrub(tc.in); got != tc.want {
			t.Errorf("%s: Scrub(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestClipDetail_ScrubsThenClips(t *testing.T) {
	tok := "ghp_" + strings.Repeat("Z", 40)
	got := ClipDetail("  " + tok + strings.Repeat("é", 400))
	if strings.Contains(got, "ghp_") {
		t.Fatalf("token leaked: %q", got)
	}
	if !strings.HasSuffix(got, "…") || len(got) > 300+len("…") {
		t.Fatalf("len %d, suffix ok=%v", len(got), strings.HasSuffix(got, "…"))
	}
	if !strings.HasPrefix(got, "[redacted]") {
		t.Fatalf("got %q", got)
	}
	if short := ClipDetail(" hi \n"); short != "hi" {
		t.Fatalf("short = %q", short)
	}
}

func TestTailLog_ByteCutStartsOnALineBoundary(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	log := strings.Repeat(line, 400) // 40000 bytes, 400 lines: line cut first, then byte cut
	out, cut := TailLog(log)
	if !cut || len(out) > CheckLogMaxBytes {
		t.Fatalf("cut=%v len=%d", cut, len(out))
	}
	for _, l := range strings.Split(out, "\n") {
		if len(l) != 99 {
			t.Fatalf("partial line %q (%d bytes) in the kept tail", l, len(l))
		}
	}
}
