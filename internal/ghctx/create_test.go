package ghctx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

type stdinCall struct {
	dir   string
	stdin string
	args  []string
}

// createClient builds a Client whose gh runners are stubs: repo/pr view on the
// plain runner, pr create on the stdin runner.
func createClient(t *testing.T, createOut string, createErr error) (*Client, *[]stdinCall, *stubRunner) {
	t.Helper()
	plain := prStub(t, "[]")
	var calls []stdinCall
	c := New(WithRunner(plain.run), WithStdinRunner(func(_ context.Context, dir, stdin string, args ...string) (string, error) {
		calls = append(calls, stdinCall{dir, stdin, args})
		return createOut, createErr
	}))
	return c, &calls, plain
}

func TestGhctx_CreatePR_ArgvAndStdin(t *testing.T) {
	c, calls, plain := createClient(t, "Warning: 1 uncommitted change\nhttps://github.com/spacingmind/smind/pull/5\n", nil)
	body := "## Summary\n- one\n\"quoted\" $(rm -rf /) `x`\n"
	pr, err := c.CreatePR(context.Background(), "/w", forge.CreatePRInput{
		Base: "develop", Head: "smind/task-1", Title: "Add; a thing", Body: body, Draft: true,
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("create calls = %d, want 1", len(*calls))
	}
	call := (*calls)[0]
	want := []string{"pr", "create", "--base", "develop", "--head", "smind/task-1", "--title", "Add; a thing", "--draft", "--body-file", "-"}
	if strings.Join(call.args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %v, want %v", call.args, want)
	}
	if call.stdin != body || call.dir != "/w" {
		t.Fatalf("stdin = %q dir = %q, want the body on stdin in /w", call.stdin, call.dir)
	}
	for _, a := range call.args {
		if strings.Contains(a, "Summary") || strings.Contains(a, "rm -rf") {
			t.Fatalf("body leaked into argv: %v", call.args)
		}
	}
	// The result is the full PR from `gh pr view 5`.
	if pr.Number != 5 || pr.URL != "https://github.com/spacingmind/smind/pull/5" || pr.Repo != "spacingmind/smind" {
		t.Fatalf("pr = %+v", pr)
	}
	if v := plain.callsWith("pr"); len(v) != 1 || v[0].args[1] != "view" || v[0].args[2] != "5" {
		t.Fatalf("pr calls = %+v, want one `pr view 5`", v)
	}
}

func TestGhctx_CreatePR_NoDraftFlagWhenNotDraft(t *testing.T) {
	c, calls, _ := createClient(t, "https://github.com/spacingmind/smind/pull/5\n", nil)
	if _, err := c.CreatePR(context.Background(), "/w", forge.CreatePRInput{Base: "develop", Head: "h", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range (*calls)[0].args {
		if a == "--draft" {
			t.Fatalf("argv has --draft for a non-draft PR: %v", (*calls)[0].args)
		}
	}
}

func TestGhctx_CreatePR_RejectsHostileURLAndBadInput(t *testing.T) {
	for _, out := range []string{
		"javascript:alert(1)\n",
		"https://evil.example.com/spacingmind/smind/pull/5\n",
		"http://github.com/spacingmind/smind/pull/5\n",
		"https://user:pw@github.com/spacingmind/smind/pull/5\n",
		"https://github.com/spacingmind/smind/issues/5\n",
		"",
	} {
		c, _, plain := createClient(t, out, nil)
		_, err := c.CreatePR(context.Background(), "/w", forge.CreatePRInput{Base: "develop", Head: "h", Title: "t"})
		var fe *forge.Error
		if !errors.As(err, &fe) || fe.Code != forge.CodeError {
			t.Fatalf("output %q: err = %v, want forge_error", out, err)
		}
		if len(plain.callsWith("pr")) != 0 {
			t.Fatalf("output %q: a pr view ran for an untrusted URL", out)
		}
	}
	c, calls, _ := createClient(t, "https://github.com/spacingmind/smind/pull/5", nil)
	for _, in := range []forge.CreatePRInput{
		{Base: "", Head: "h", Title: "t"}, {Base: "b", Head: "", Title: "t"}, {Base: "b", Head: "h", Title: ""},
		{Base: "-x", Head: "h", Title: "t"}, {Base: "b", Head: "--head", Title: "t"},
	} {
		if _, err := c.CreatePR(context.Background(), "/w", in); err == nil {
			t.Fatalf("CreatePR(%+v) succeeded", in)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("gh ran for invalid input: %+v", *calls)
	}
}

func TestGhctx_CreatePR_ErrorsAreTypedAndScrubbed(t *testing.T) {
	c, _, _ := createClient(t, "", errors.New("gh: HTTP 401: Bad credentials token ghp_abcdefghijklmnopqrstuvwxyz0123456789"))
	_, err := c.CreatePR(context.Background(), "/w", forge.CreatePRInput{Base: "develop", Head: "h", Title: "secret title", Body: "secret body"})
	var fe *forge.Error
	if !errors.As(err, &fe) || fe.Code != forge.CodeUnauthenticated {
		t.Fatalf("err = %v, want forge_unauthenticated", err)
	}
	if s := fe.Message + fe.Detail; strings.Contains(s, "ghp_") || strings.Contains(s, "secret") {
		t.Fatalf("error leaks token/argv/body: %q", s)
	}
}

// TestDefaultStdinRunner_BodyOnStdin proves the default runner feeds stdin to
// the real process (a fake gh on PATH echoes its stdin and argv).
func TestDefaultStdinRunner_BodyOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh doesn't run on windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'ARGS:%s\\n' \"$*\"\ncat\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := defaultStdinRunner(context.Background(), dir, "line1\n$(touch pwned)\n", "pr", "create", "--body-file", "-")
	if err != nil {
		t.Fatalf("defaultStdinRunner: %v", err)
	}
	if want := "ARGS:pr create --body-file -\nline1\n$(touch pwned)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("stdin was interpreted by a shell")
	}
}
