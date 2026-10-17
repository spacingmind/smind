package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/logging"
	"github.com/spacingmind/smind/internal/service"
)

type scripted struct {
	codes []int
	calls int
}

func (s *scripted) start(context.Context) (int, error) {
	c := s.codes[s.calls]
	s.calls++
	return c, nil
}

func collect() (*[]string, func(string, ...any)) {
	var lines []string
	return &lines, func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
}

// The task's own RestartOnFailure never sees a crash (conhost --headless exits 0
// whatever the daemon's code, measured on windows-latest), so `service run`
// restarts a crashed daemon itself: 3 times, delay apart.
func TestSuperviseRestartsACrashedDaemon(t *testing.T) {
	s := &scripted{codes: []int{1, 137, 0}}
	lines, logf := collect()
	if code := supervise(context.Background(), s.start, 3, time.Millisecond, time.Hour, logf); code != 0 {
		t.Errorf("exit = %d, want 0 once the daemon exits cleanly", code)
	}
	if s.calls != 3 {
		t.Errorf("started %d times, want 3 (two restarts)", s.calls)
	}
	if len(*lines) != 2 || !strings.Contains((*lines)[0], "restart 1 of 3") || !strings.Contains((*lines)[1], "code 137") {
		t.Errorf("log = %q", *lines)
	}
}

func TestSuperviseGivesUpAfterTheRestartBudget(t *testing.T) {
	s := &scripted{codes: []int{1, 1, 1, 1, 1}}
	lines, logf := collect()
	code := supervise(context.Background(), s.start, 3, time.Millisecond, time.Hour, logf)
	if code != 1 || s.calls != 4 {
		t.Errorf("exit = %d after %d starts, want exit 1 after 1+3 starts", code, s.calls)
	}
	if last := (*lines)[len(*lines)-1]; !strings.Contains(last, "giving up after 3 restarts") {
		t.Errorf("last log line = %q", last)
	}
}

// `smind stop` exits 0: a clean stop is final, nothing is restarted.
func TestSuperviseDoesNotRestartAfterAGracefulStop(t *testing.T) {
	s := &scripted{codes: []int{0, 1}}
	lines, logf := collect()
	if code := supervise(context.Background(), s.start, 3, time.Millisecond, time.Hour, logf); code != 0 || s.calls != 1 || len(*lines) != 0 {
		t.Errorf("exit = %d, %d starts, log %q", code, s.calls, *lines)
	}
}

func TestSuperviseStopsWaitingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &scripted{codes: []int{1, 1}}
	_, logf := collect()
	done := make(chan int, 1)
	go func() {
		done <- supervise(ctx, s.start, 3, time.Hour, time.Hour, logf) // would wait an hour before restarting
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 1 || s.calls != 1 {
			t.Errorf("exit = %d, %d starts", code, s.calls)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervise did not return after cancel")
	}
}

func TestSuperviseTreatsAStartFailureAsACrash(t *testing.T) {
	calls := 0
	lines, logf := collect()
	code := supervise(context.Background(), func(context.Context) (int, error) {
		calls++
		return 0, errors.New("file not found")
	}, 1, time.Millisecond, time.Hour, logf)
	if code != 1 || calls != 2 || !strings.Contains((*lines)[0], "could not start the daemon") {
		t.Errorf("exit = %d, %d calls, log %q", code, calls, *lines)
	}
}

func TestServiceRunSetsTheSupervisorAndHome(t *testing.T) {
	t.Setenv(service.SupervisorEnv, "")
	t.Setenv("SMIND_HOME", "")
	home := t.TempDir()
	if err := applyServiceRunEnv(home); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(service.SupervisorEnv) != "taskscheduler" || os.Getenv("SMIND_HOME") != home {
		t.Errorf("SMIND_SUPERVISOR=%q SMIND_HOME=%q", os.Getenv(service.SupervisorEnv), os.Getenv("SMIND_HOME"))
	}
	if err := applyServiceRunEnv("relative/dir"); err == nil {
		t.Error("a relative --home must be rejected")
	}
	// No --home leaves SMIND_HOME alone.
	t.Setenv("SMIND_HOME", "keep")
	_ = applyServiceRunEnv("")
	if os.Getenv("SMIND_HOME") != "keep" {
		t.Error("SMIND_HOME must not be touched without --home")
	}
}

func TestServiceRunUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"service", "run", "extra"}, {"service", "run", "--port", "1"}, {"service", "run", "--home", "rel"}} {
		if code, _ := runSvc(t, args); code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
	}
}

func TestSuperviseLogLinesGoToDaemonLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), logging.FileName)
	appendDaemonLog(path, "service run: the daemon exited with code %d", 1)
	appendDaemonLog(path, "second")
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "exited with code 1") || !strings.HasSuffix(lines[1], " second") {
		t.Errorf("daemon.log = %q", b)
	}
}

// A daemon that ran healthily before it crashed starts the restart budget over:
// three isolated crashes spread over a year must not leave it down for good.
func TestSuperviseBudgetResetsAfterAHealthyRun(t *testing.T) {
	calls := 0
	lines, logf := collect()
	start := func(context.Context) (int, error) {
		calls++
		if calls <= 6 {
			time.Sleep(15 * time.Millisecond) // "ran healthily" for the 10 ms threshold below
			return 1, nil
		}
		return 0, nil
	}
	if code := supervise(context.Background(), start, 3, time.Millisecond, 10*time.Millisecond, logf); code != 0 || calls != 7 {
		t.Errorf("exit = %d after %d starts: six healthy-then-crash runs must all be restarted", code, calls)
	}
	for _, l := range *lines {
		if strings.Contains(l, "giving up") {
			t.Errorf("gave up despite healthy runs: %q", *lines)
		}
	}
	// A crash loop (instant failures) still exhausts the budget.
	loop := 0
	code := supervise(context.Background(), func(context.Context) (int, error) { loop++; return 1, nil }, 3, time.Millisecond, 10*time.Millisecond, logf)
	if code != 1 || loop != 4 {
		t.Errorf("crash loop: exit %d after %d starts", code, loop)
	}
}
