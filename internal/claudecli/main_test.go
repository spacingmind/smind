package claudecli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// Environment switches for the test binary's roles. The launcher tests run
// this very binary three ways: as the launcher (LaunchEnv set, handled by
// MaybeRunLauncher), as a fake claude.exe (fakeClaudeEnv), and as a
// long-lived grandchild (fakeSleepEnv).
const (
	fakeClaudeEnv     = "CLAUDECLI_FAKE_CLAUDE"      // act as claude.exe
	fakeExitEnv       = "CLAUDECLI_FAKE_EXIT"        // exit code to return
	fakeGrandchildEnv = "CLAUDECLI_FAKE_GRANDCHILD"  // file to write the grandchild pid to
	fakeLingerEnv     = "CLAUDECLI_FAKE_LINGER"      // keep running after setup
	fakeSleepEnv      = "CLAUDECLI_FAKE_SLEEP"       // act as the grandchild
	fakeBootNoteEnv   = "CLAUDECLI_FAKE_BOOT_MARKER" // present only to prove env pass-through
)

func TestMain(m *testing.M) {
	// Order matters: the launcher inherits the fake-claude env vars (it has
	// to pass them down to the child), so it must be recognised first, by
	// LaunchEnv, which the child never sees.
	if code, handled := MaybeRunLauncher(); handled {
		os.Exit(code)
	}
	if os.Getenv(fakeSleepEnv) != "" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if os.Getenv(fakeClaudeEnv) != "" {
		os.Exit(runFakeClaude())
	}
	os.Exit(m.Run())
}

// runFakeClaude is a stand-in claude.exe: it reports how it was called as
// one JSON line, echoes stdin, optionally starts a long-lived grandchild
// (recording its pid) and lingers, and exits with the requested code.
func runFakeClaude() int {
	in, _ := io.ReadAll(limitedStdin())
	report := map[string]any{
		"args":             os.Args[1:],
		"launchEnvPresent": os.Getenv(LaunchEnv) != "",
		"bootMarker":       os.Getenv(fakeBootNoteEnv),
		"stdin":            string(in),
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)

	if pidFile := os.Getenv(fakeGrandchildEnv); pidFile != "" {
		gc := exec.Command(os.Args[0])
		gc.Env = append(os.Environ(), fakeSleepEnv+"=1")
		if err := gc.Start(); err == nil {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(gc.Process.Pid)), 0o600)
		}
	}
	if os.Getenv(fakeLingerEnv) != "" {
		time.Sleep(time.Minute)
	}
	if v := os.Getenv(fakeExitEnv); v != "" {
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// limitedStdin returns stdin unless a lingering/grandchild scenario is
// running (those tests never write to stdin and must not block on it).
func limitedStdin() io.Reader {
	if os.Getenv(fakeLingerEnv) != "" || os.Getenv(fakeGrandchildEnv) != "" {
		return eofReader{}
	}
	return os.Stdin
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

var _ = testing.Short // keep the testing import even when only TestMain uses it
