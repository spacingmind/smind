package ghctx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

func TestErrorMapping_Table(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	c := New(WithNow(func() time.Time { return now }))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	bg := context.Background()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		code string
	}{
		{"exec not found", bg, exec.ErrNotFound, forge.CodeCLIMissing},
		{"executable file not found", bg, errors.New(`exec: "gh": executable file not found in $PATH`), forge.CodeCLIMissing},
		{"auth login", bg, errors.New("gh: To get started with GitHub CLI, please run: gh auth login"), forge.CodeUnauthenticated},
		{"not logged in", bg, errors.New("gh: not logged in"), forge.CodeUnauthenticated},
		{"authentication required", bg, errors.New("gh: authentication required"), forge.CodeUnauthenticated},
		{"http 401", bg, errors.New("gh: HTTP 401: Bad credentials"), forge.CodeUnauthenticated},
		{"no git remotes", bg, errors.New("gh: no git remotes found"), forge.CodeNoRemote},
		{"none of the git remotes", bg, errors.New("gh: none of the git remotes point to a known GitHub host"), forge.CodeNoRemote},
		{"could not determine base repo", bg, errors.New("gh: could not determine base repo"), forge.CodeNoRemote},
		{"not a git repository", bg, errors.New("gh: not a git repository"), forge.CodeNoRemote},
		{"unsupported host", bg, errors.New("gh: unsupported host gitlab.com"), forge.CodeNoRemote},
		{"could not resolve to", bg, errors.New("gh: Could not resolve to a PullRequest with the number 999"), forge.CodeNotFound},
		{"http 404", bg, errors.New("gh: HTTP 404 Not Found"), forge.CodeNotFound},
		{"no pull requests match", bg, errors.New("gh: no pull requests match"), forge.CodeNotFound},
		{"rate limit", bg, errors.New("gh: API rate limit exceeded"), forge.CodeRateLimited},
		{"http 429", bg, errors.New("gh: HTTP 429"), forge.CodeRateLimited},
		{"secondary 403", bg, errors.New("gh: HTTP 403: You have exceeded a secondary rate limit"), forge.CodeRateLimited},
		{"timeout cancelled ctx", cancelled, errors.New("gh: signal: killed"), forge.CodeTimeout},
		{"timeout deadline err", bg, context.DeadlineExceeded, forge.CodeTimeout},
		{"other", bg, errors.New("gh: some other failure: boom"), forge.CodeError},
	}
	if got := c.mapRunError(bg, nil); got != nil {
		t.Fatalf("mapRunError(nil) = %v, want nil", got)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fe *forge.Error
			if err := c.mapRunError(tc.ctx, tc.err); !errors.As(err, &fe) {
				t.Fatalf("mapRunError(%v) = %v, want *forge.Error", tc.err, err)
			}
			if fe.Code != tc.code || fe.RPCCode() != tc.code {
				t.Fatalf("code = %q, want %q", fe.Code, tc.code)
			}
			if fe.Message == "" || strings.Contains(fe.Message, "boom") {
				t.Fatalf("message = %q", fe.Message)
			}
			if tc.code == forge.CodeRateLimited && !fe.RetryAt.Equal(now.Add(10*time.Minute)) {
				t.Fatalf("RetryAt = %v, want now+10m", fe.RetryAt)
			}
			if tc.code == forge.CodeError && !strings.Contains(fe.Detail, "boom") {
				t.Fatalf("Detail = %q, want the clipped error text", fe.Detail)
			}
		})
	}
}

func TestErrorMapping_RateLimitRetryHints(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	c := New(WithNow(func() time.Time { return now }))
	var fe *forge.Error
	if !errors.As(c.mapRunError(context.Background(), errors.New("gh: API rate limit exceeded, retry after 90 seconds")), &fe) || !fe.RetryAt.Equal(now.Add(90*time.Second)) {
		t.Fatalf("retry-after: %+v", fe)
	}
	if !errors.As(c.mapRunError(context.Background(), errors.New("gh: rate limit exceeded; reset at 1790000000")), &fe) || !fe.RetryAt.Equal(time.Unix(1790000000, 0)) {
		t.Fatalf("reset: %+v", fe)
	}
}

func TestRunnerError_NeverContainsArgv(t *testing.T) {
	stub := func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", errors.New("gh: exit status 1: boom")
	}
	c := New(WithRunner(stub))
	_, err := c.Search(context.Background(), t.TempDir(), "pr", "secretquery", 5)
	var fe *forge.Error
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *forge.Error", err)
	}
	for _, s := range []string{fe.Error(), fe.Detail} {
		if strings.Contains(s, "secretquery") || strings.Contains(s, "--json") {
			t.Fatalf("argv leaked: %q", s)
		}
	}
}

// TestDefaultRunner_ErrorHasNoArgv runs the real default runner against a
// fake failing gh and checks the error carries stderr but not argv.
func TestDefaultRunner_ErrorHasNoArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh doesn't run on windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'bad ghp_" + strings.Repeat("q", 30) + " Authorization: token abc' >&2\nexit 3\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := defaultRunner(context.Background(), dir, "pr", "list", "--search", "needle")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "needle") || strings.Contains(err.Error(), "--search") {
		t.Fatalf("argv in error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("error = %q, want exit status", err.Error())
	}
}

func TestRunnerError_ScrubsTokens(t *testing.T) {
	tok := "ghp_" + strings.Repeat("x", 30)
	stub := func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", errors.New("gh: exit status 1: " + forge.Scrub("bad "+tok+"\nAuthorization: token abc"))
	}
	c := New(WithRunner(stub))
	_, err := c.Get(context.Background(), t.TempDir(), "pr", 1, "o/r")
	var fe *forge.Error
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{fe.Error(), fe.Detail} {
		if strings.Contains(s, tok) || strings.Contains(s, "token abc") {
			t.Fatalf("secret leaked: %q", s)
		}
	}
	// The mapper scrubs on its own too, for runners that don't.
	raw := errors.New("gh: exit status 1: " + tok + " Authorization: token abc")
	fe = c.mapRunError(context.Background(), raw).(*forge.Error)
	if strings.Contains(fe.Detail, tok) || strings.Contains(fe.Detail, "token abc") {
		t.Fatalf("Detail leaked: %q", fe.Detail)
	}
}

func TestGhctx_ImplementsForgeProvider(t *testing.T) {
	var p forge.Provider = New()
	_ = p.Repo
	_ = p.OpenPRs
	_ = p.PR
	if p.Name() != "github" {
		t.Fatalf("Name() = %q", p.Name())
	}
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
