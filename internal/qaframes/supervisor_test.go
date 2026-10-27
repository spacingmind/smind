package qaframes

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestHelperSleep is not a test: it is the child process the supervisor tests
// start (the test binary itself, so it runs on every OS without a shell).
func TestHelperSleep(t *testing.T) {
	if os.Getenv("QAFRAMES_HELPER") != "sleep" {
		t.Skip("helper process only") // not a real skip: runs only as the child
	}
	time.Sleep(time.Minute)
}

func TestSupervisor_KillAll_StopsStartedChild_AllOSes(t *testing.T) {
	sup := NewSupervisor()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "QAFRAMES_HELPER=sleep")
	if err := sup.Start(cmd); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if got := sup.PIDs(); len(got) != 1 || got[0] != pid {
		t.Fatalf("PIDs = %v, want [%d]", got, pid)
	}
	if !Alive(pid) {
		t.Fatalf("child %d should be alive before KillAll", pid)
	}
	sup.KillAll()
	for i := 0; i < 60 && Alive(pid); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if Alive(pid) {
		t.Fatalf("child %d survived KillAll: an orphan", pid)
	}
	sup.KillAll() // idempotent
}
