package taskrunner

import (
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/scratch"
)

// lastEnvEntry returns the last "NAME=" line of an environment dump the
// fake agents write (os.Environ() joined by newlines): duplicate names are
// possible when a launch prepends entries after the inherited environment,
// and the last one is the value that wins.
func lastEnvEntry(t *testing.T, env, name string) string {
	t.Helper()
	last := ""
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, name+"=") {
			last = line
		}
	}
	if last == "" {
		t.Fatalf("agent environment has no %s entry:\n%s", name, env)
	}
	return last
}

// AC1, claude-native: the launch env points TMPDIR at the task's scratch
// dir, and the dir exists before the CLI runs.
func TestLaunchEnv_ScratchTmpdir_Claude(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithScratch(scratch.New(t.TempDir()))(r)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	dir := r.scratch.Dir(task.ID)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("scratch dir %s missing or not a directory: %v", dir, err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "TMPDIR"); got != "TMPDIR="+dir {
		t.Fatalf("TMPDIR = %q, want the scratch dir %q", got, dir)
	}
}

// AC1: a TMPDIR pinned in the provider env wins over the scratch entry
// (scratch entries are prepended; last entry wins).
func TestLaunchEnv_PinnedTmpdirWins(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithScratch(scratch.New(t.TempDir()))(r)
	r.SetProviderEnv(ProviderClaudeNative, "TMPDIR", "/pinned/tmp")

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "TMPDIR"); got != "TMPDIR=/pinned/tmp" {
		t.Fatalf("TMPDIR = %q, want the pinned /pinned/tmp (last wins)", got)
	}
}

// AC2: configured shared caches reach the launch env, scratch or not.
func TestSharedCaches_On(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithSharedCaches(scratch.SharedCaches{"GOCACHE": "/shared/gocache", "GOMODCACHE": "/shared/mod"})(r)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	env := readLaunchEnv(t, task)
	for _, want := range []string{"GOCACHE=/shared/gocache", "GOMODCACHE=/shared/mod"} {
		if !strings.Contains(env, want) {
			t.Fatalf("agent environment lacks %q:\n%s", want, env)
		}
	}
}

// AC2: with no WithSharedCaches option nothing is added, even when the
// daemon's own environment sets the name.
func TestSharedCaches_Off(t *testing.T) {
	os.Unsetenv("GOCACHE")
	r, _, task := newClaudeMcpEnv(t)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	env := readLaunchEnv(t, task)
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, "GOCACHE=") {
			t.Fatalf("agent environment has %q with shared caches off:\n%s", line, env)
		}
	}
}

// AC2: a provider-pinned cache var wins over the shared-cache entry.
func TestSharedCaches_DoesNotOverrideExisting(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithSharedCaches(scratch.SharedCaches{"GOCACHE": "/shared/gocache"})(r)
	r.SetProviderEnv(ProviderClaudeNative, "GOCACHE", "/mine")

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "GOCACHE"); got != "GOCACHE=/mine" {
		t.Fatalf("GOCACHE = %q, want the pinned /mine (last wins)", got)
	}
}
