package workspace

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

func TestPushError_Classification(t *testing.T) {
	bg := context.Background()
	exit := errors.New("exit status 1")
	cases := []struct {
		name, stderr, remote, code string
	}{
		{"non fast forward", " ! [rejected] a -> a (non-fast-forward)\nerror: failed to push some refs", "https://github.com/o/r.git", forge.CodeNonFastForward},
		{"fetch first", " ! [rejected] a -> a (fetch first)", "git@github.com:o/r.git", forge.CodeNonFastForward},
		{"hook decline", " ! [remote rejected] a -> a (pre-receive hook declined)", "https://github.com/o/r.git", forge.CodePushRejected},
		{"protected branch", " ! [remote rejected] a -> a (protected branch hook declined)", "https://github.com/o/r.git", forge.CodePushRejected},
		{"https auth", "fatal: Authentication failed for 'https://github.com/o/r.git/'", "https://github.com/o/r.git", forge.CodeUnauthenticated},
		{"ssh auth", "git@github.com: Permission denied (publickey).", "git@github.com:o/r.git", forge.CodePushAuth},
		{"other", "fatal: something odd", "https://github.com/o/r.git", forge.CodeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if pe := pushError(bg, exit, tc.stderr, tc.remote); pe.Code != tc.code {
				t.Fatalf("code = %q, want %q", pe.Code, tc.code)
			}
		})
	}
}

func TestSync_NoForceFlagAnywhere(t *testing.T) {
	b, err := os.ReadFile("sync.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"--force", "force-with-lease", "--mirror", "--delete", "+refs"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("sync.go contains %q", bad)
		}
	}
}

// The push refspec is always plain ref:ref, whatever the names: a leading '+'
// would force the push.
func TestPushRefspecNeverForced(t *testing.T) {
	for _, tc := range []struct{ branch, dest, want string }{
		{"smind/task-a", "smind/task-a", "refs/heads/smind/task-a:refs/heads/smind/task-a"},
		{"smind/pr-7-x", "feat/x", "refs/heads/smind/pr-7-x:refs/heads/feat/x"},
		{"+odd", "+odd", "refs/heads/+odd:refs/heads/+odd"},
	} {
		got := pushRefspec(tc.branch, tc.dest)
		if got != tc.want || strings.HasPrefix(got, "+") || strings.Contains(got, ":+") {
			t.Fatalf("pushRefspec(%q, %q) = %q, want %q", tc.branch, tc.dest, got, tc.want)
		}
	}
}

func TestPushDest(t *testing.T) {
	repo := t.TempDir()
	if err := runGit(repo, "init", "-q"); err != nil {
		t.Skipf("git init: %v", err)
	}
	for _, tc := range []struct{ head, want string }{
		{"", "own"},
		{"feat/x", "feat/x"},
		{forge.CherryPickBranchPrefix + "4", "own"},
		{"-bad", "own"},
		{"has space", "own"},
	} {
		if got := pushDest(repo, "own", tc.head); got != tc.want {
			t.Errorf("pushDest(head %q) = %q, want %q", tc.head, got, tc.want)
		}
	}
}

func TestValidRemoteName(t *testing.T) {
	for name, want := range map[string]bool{
		"fork": true, "my-fork_2": true, "team/fork": true,
		"": false, "-x": false, "--upload-pack=x": false, "a b": false, "a..b": false, "a;b": false, "$(x)": false,
	} {
		if got := validRemoteName(name); got != want {
			t.Errorf("validRemoteName(%q) = %v, want %v", name, got, want)
		}
	}
}
