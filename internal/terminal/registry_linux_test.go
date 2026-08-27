//go:build linux

package terminal

import (
	"strings"
	"testing"
	"time"
)

// TestRegistry_Close_KillsBackgroundJobsToo proves Close's process
// cleanup reaches further than just the shell's own pid: a background job
// started inside an interactive bash session (job control gives it its
// own process group, distinct from the shell's -- see killTree's doc
// comment in kill_linux.go) must not survive Close as an orphan. This is
// the concrete scenario killTree's /proc-tree walk exists for, as opposed
// to a plain "kill -pgid" which would miss it.
func TestRegistry_Close_KillsBackgroundJobsToo(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)
	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	shellPid := pidOf(t, reg, id)

	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	// Start a long-running background job and echo a marker once it's
	// launched, so we know the job (and thus its own process group,
	// distinct from the shell's) genuinely exists before we try to kill
	// it.
	if err := reg.Write(id, []byte("sleep 300 & echo job-started-$!\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Can't just wait for the "job-started-" substring to appear: the PTY
	// echoes typed input back before the shell even runs it, so that
	// substring shows up in the echoed command line itself, with no pid
	// number after it yet. Wait until a pid can actually be parsed out of
	// what's accumulated so far instead.
	var out string
	var jobPid int
	deadline := time.After(5 * time.Second)
	for {
		var ok bool
		jobPid, ok = parseJobPid(out)
		if ok {
			break
		}
		select {
		case e, chOk := <-events:
			if !chOk {
				t.Fatalf("events closed before a background job pid appeared; got so far: %q", out)
			}
			out += string(e.Data)
		case <-deadline:
			t.Fatalf("timed out waiting for a background job pid; got so far: %q", out)
		}
	}
	if !processAlive(jobPid) {
		t.Fatalf("background job pid %d not alive right after starting it", jobPid)
	}

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	waitGone(t, shellPid, 2*time.Second)
	waitGone(t, jobPid, 2*time.Second)
}

// parseJobPid extracts the numeric pid bash printed after
// "job-started-" (from `echo job-started-$!`, where $! is the most
// recently backgrounded job's pid).
func parseJobPid(out string) (int, bool) {
	idx := strings.LastIndex(out, "job-started-")
	if idx < 0 {
		return 0, false
	}
	rest := out[idx+len("job-started-"):]
	end := strings.IndexAny(rest, "\r\n")
	if end >= 0 {
		rest = rest[:end]
	}
	rest = strings.TrimSpace(rest)
	n := 0
	if rest == "" {
		return 0, false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
