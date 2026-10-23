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
