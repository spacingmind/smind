package runs

import (
	"strings"
	"testing"
)

func TestSetupBusyErrorNamesCLIFlag(t *testing.T) {
	t.Parallel()
	msg := (&SetupBusyError{TaskID: 7}).Error()
	for _, want := range []string{"task 7's worktree setup script is still running", "whenBusy=queue", "--when-busy queue"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}
