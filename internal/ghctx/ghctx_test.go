package ghctx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestErrorMapping_Table(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    string
		message string
		typed   bool
	}{
		{"nil", nil, "", "", false},
		{"exec not found", exec.ErrNotFound, CodeUnavailable, "gh is not installed on the daemon host", true},
		{"executable file not found", errors.New(`exec: "gh": executable file not found in $PATH`), CodeUnavailable, "gh is not installed on the daemon host", true},
		{"auth login", errors.New("gh: To get started with GitHub CLI, please run: gh auth login"), CodeUnavailable, "gh is not logged in on the daemon host (run gh auth login)", true},
		{"not logged in", errors.New("gh: not logged in"), CodeUnavailable, "gh is not logged in on the daemon host (run gh auth login)", true},
		{"authentication required", errors.New("gh: authentication required"), CodeUnavailable, "gh is not logged in on the daemon host (run gh auth login)", true},
		{"no git remotes", errors.New("gh: no git remotes found"), CodeUnavailable, "this workspace has no GitHub remote", true},
		{"none of the git remotes", errors.New("gh: none of the git remotes point to a known GitHub host"), CodeUnavailable, "this workspace has no GitHub remote", true},
		{"could not determine base repo", errors.New("gh: could not determine base repo"), CodeUnavailable, "this workspace has no GitHub remote", true},
		{"not a git repository", errors.New("gh: not a git repository"), CodeUnavailable, "this workspace has no GitHub remote", true},
		{"could not resolve to", errors.New("gh: Could not resolve to a PullRequest with the number 999"), CodeNotFound, "pull request or issue not found", true},
		{"not found", errors.New("gh: HTTP 404 Not Found"), CodeNotFound, "pull request or issue not found", true},
		{"no pull requests match", errors.New("gh: no pull requests match"), CodeNotFound, "pull request or issue not found", true},
		{"other", errors.New("gh: some other failure: boom"), "", "", false},
	}
	for _, tc := range cases {
		if tc.err == nil {
			if got := mapRunError(nil); got != nil {
				t.Fatalf("mapRunError(nil) = %v, want nil", got)
			}
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			got := mapRunError(tc.err)
			if !tc.typed {
				if got == nil || isTypedError(got) {
					t.Fatalf("mapRunError(%v) = %v, want a non-typed error", tc.err, got)
				}
				return
			}
			var te *Error
			if !errors.As(got, &te) {
				t.Fatalf("mapRunError(%v) = %v, want a typed *Error", tc.err, got)
			}
			if te.Code != tc.code {
				t.Fatalf("code = %q, want %q", te.Code, tc.code)
			}
			if te.Message != tc.message {
				t.Fatalf("message = %q, want %q", te.Message, tc.message)
			}
			if te.RPCCode() != tc.code {
				t.Fatalf("RPCCode() = %q, want %q", te.RPCCode(), tc.code)
			}
		})
	}
}

func isTypedError(err error) bool {
	var te *Error
	return errors.As(err, &te)
}

// TestDefaultRunner_ArgvOnlyNoShell proves the default runner passes args
// as argv to the gh binary (never through a shell): a fake gh on PATH
// echoes its own argv back as JSON, and a query containing shell
// metacharacters arrives as a single literal argument.
func TestDefaultRunner_ArgvOnlyNoShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh doesn't run on windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s
' "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := defaultRunner(context.Background(), dir, "pr", "list", "--search", `a;rm -rf /b && echo pwned`)
	if err != nil {
		t.Fatalf("defaultRunner() error = %v", err)
	}
	if want := "pr\nlist\n--search\na;rm -rf /b && echo pwned\n"; got != want {
		t.Fatalf("defaultRunner stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "b")); err == nil {
		t.Fatal("shell metacharacters were interpreted; runner is not argv-only")
	}
}
