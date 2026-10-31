package permpolicy

import (
	"errors"
	"strings"
	"testing"
)

func winLoad(doc string) (*Policy, error) {
	return loadPolicy([]byte(doc), windowsPlatform(&fakeFS{}, nil))
}

// goTestWithin is a Bash allow for go test scoped to within (a mutating Edit allow
// trips the no-destructive-outside floor probe).
func goTestWithin(within string) string {
	return policyDoc("- id: r\n  action: allow\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: go\n      subcommand: test\n    paths:\n      within: [" + within + "]")
}

func TestPolicyLoadWindows_RejectsRelativeWithin(t *testing.T) {
	t.Parallel()
	for _, within := range []string{`src\pkg`, `\rooted`, `C:foo`, `/etc/x`, `C:\a\..\b`, `C:\a\*`, `\\srv`} {
		_, err := winLoad(goTestWithin(`'` + within + `'`))
		if err == nil {
			t.Errorf("within %q: accepted, want rejection", within)
			continue
		}
		var le *LoadError
		if !errors.As(err, &le) {
			t.Errorf("within %q: error %v is %T, want *LoadError", within, err, err)
		}
		if !strings.Contains(err.Error(), "rules[0].match.paths.within[0]") {
			t.Errorf("within %q: error %q lacks the field path", within, err)
		}
	}
}

func TestPolicyLoadWindows_AcceptsDriveAndUNC(t *testing.T) {
	t.Parallel()
	for _, within := range []string{`C:\Work\Wt`, `d:/work/wt`, `\\srv\share\x`, `{worktree}`} {
		if _, err := winLoad(goTestWithin(`'` + within + `'`)); err != nil {
			t.Errorf("within %q: rejected: %v", within, err)
		}
	}
}
