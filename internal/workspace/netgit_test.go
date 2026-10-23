package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

func useNetworkGit(t *testing.T, fn NetworkGitConfig) {
	t.Helper()
	SetNetworkGitConfig(fn)
	t.Cleanup(func() { SetNetworkGitConfig(nil) })
}

// TestNetworkGit_HookReachesFetchAndPush: the options the hook returns are
// part of the very `git fetch` and `git push` the PR path runs. The hook
// points origin's push URL at a second bare repo with a per-invocation -c,
// so where the branch lands proves the option reached git.
func TestNetworkGit_HookReachesFetchAndPush(t *testing.T) {
	m, task, _, bare := prTask(t)
	other := newBareRepo(t)
	writeAndCommit(t, *task.WorktreePath, "a.txt", "a\n", "a")

	var mu sync.Mutex
	var remotes []string
	useNetworkGit(t, func(_ context.Context, dir, remote string) []string {
		mu.Lock()
		remotes = append(remotes, remote)
		mu.Unlock()
		return []string{"-c", "remote.origin.pushurl=" + other}
	})
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatalf("PushForPR: %v", err)
	}
	if remoteBranchSHA(t, other, *task.Branch) == "" {
		t.Fatalf("the push did not use the hook's -c option (branch missing from %s)", other)
	}
	if remoteBranchSHA(t, bare, *task.Branch) != "" {
		t.Fatal("the push also went to the configured origin")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(remotes) != 2 || remotes[0] != "origin" || remotes[1] != "origin" { // fetch of the base, then push
		t.Fatalf("hook calls = %v, want fetch + push for origin", remotes)
	}
}

// TestNetworkGit_NoHookNoOptions: with no hook (or an empty answer) the
// command line is exactly what it was.
func TestNetworkGit_NoHookNoOptions(t *testing.T) {
	SetNetworkGitConfig(nil)
	got := netGitArgs(context.Background(), "/d", "origin", "fetch", "origin")
	if strings.Join(got, " ") != "fetch origin" {
		t.Fatalf("args = %v", got)
	}
	useNetworkGit(t, func(context.Context, string, string) []string { return nil })
	if got := netGitArgs(context.Background(), "/d", "origin", "push", "origin"); strings.Join(got, " ") != "push origin" {
		t.Fatalf("empty hook changed args: %v", got)
	}
	useNetworkGit(t, func(context.Context, string, string) []string { return forge.CredentialHelperArgs() })
	got = netGitArgs(context.Background(), "/d", "origin", "push", "origin")
	if got[len(got)-2] != "push" || got[0] != "-c" || got[1] != "credential.helper=" || got[3] != "credential.helper=!gh auth git-credential" {
		t.Fatalf("args = %v", got)
	}
}

// TestGitNetwork_GlobalConfigUntouched: running the PR push and fetch with
// the credential helper options edits neither the user's global git config
// nor the repository's own.
func TestGitNetwork_GlobalConfigUntouched(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, "gitconfig")
	const seed = "[user]\n\tname = Someone\n\temail = s@example.com\n"
	if err := os.WriteFile(global, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	m, task, repo, _ := prTask(t)
	writeAndCommit(t, *task.WorktreePath, "a.txt", "a\n", "a")
	repoCfg := filepath.Join(repo, ".git", "config")
	before, err := os.ReadFile(repoCfg)
	if err != nil {
		t.Fatal(err)
	}

	useNetworkGit(t, func(context.Context, string, string) []string { return forge.CredentialHelperArgs() })
	if _, err := m.PushForPR(context.Background(), task.ID, ""); err != nil {
		t.Fatalf("PushForPR: %v", err)
	}
	if err := m.PushUpdate(context.Background(), task.ID, *task.Branch); err != nil {
		t.Fatalf("PushUpdate: %v", err)
	}

	if after, _ := os.ReadFile(global); string(after) != seed {
		t.Fatalf("global git config changed:\n%s", after)
	}
	if after, _ := os.ReadFile(repoCfg); string(after) != string(before) {
		t.Fatalf("repo git config changed:\n%s", after)
	}
	cmd := exec.Command("git", "config", "--local", "--get-all", "credential.helper")
	cmd.Dir = repo
	if out, _ := cmd.Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("credential.helper is now configured: %q", out)
	}
}
