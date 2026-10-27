package qaframes

import (
	"context"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// Supervisor starts child processes and records their PIDs. KillAll stops
// exactly the processes it started (each in its own process group, so their
// helpers go with them) and the PIDs it was told to adopt; it never matches
// processes by name.
type Supervisor struct {
	mu      sync.Mutex
	started map[int]chan struct{} // pid -> closed when reaped
	adopted map[int]bool
}

// NewSupervisor returns an empty Supervisor.
func NewSupervisor() *Supervisor {
	return &Supervisor{started: map[int]chan struct{}{}, adopted: map[int]bool{}}
}

// Start runs cmd in its own process group and records its PID. The child is
// reaped in the background.
func (s *Supervisor) Start(cmd *exec.Cmd) error {
	_, err := s.start(cmd)
	return err
}

// Run starts cmd like Start and waits for it, returning its exit error. A
// cancelled ctx returns ctx.Err() and leaves the child for KillAll.
func (s *Supervisor) Run(ctx context.Context, cmd *exec.Cmd) error {
	errc, err := s.start(cmd)
	if err != nil {
		return err
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) start(cmd *exec.Cmd) (<-chan error, error) {
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	errc := make(chan error, 1)
	s.mu.Lock()
	s.started[cmd.Process.Pid] = done
	s.mu.Unlock()
	go func() { errc <- cmd.Wait(); close(done) }()
	return errc, nil
}

// Adopt records a PID this tool did not start itself but whose owner it did
// (the browser a driver launched, reported by the driver). It is killed at
// KillAll only if still alive.
func (s *Supervisor) Adopt(pid int) {
	if pid <= 0 {
		return
	}
	s.mu.Lock()
	s.adopted[pid] = true
	s.mu.Unlock()
}

// PIDs lists every recorded PID, sorted.
func (s *Supervisor) PIDs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int
	for p := range s.started {
		out = append(out, p)
	}
	for p := range s.adopted {
		if _, dup := s.started[p]; !dup {
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}

// KillAll terminates the recorded processes: SIGTERM to each group, a grace
// period, then SIGKILL. It is idempotent.
func (s *Supervisor) KillAll() {
	s.mu.Lock()
	var groups, loose []int
	for p := range s.started { // a reaped child's group may still hold helpers: kill it anyway
		groups = append(groups, p)
	}
	for p := range s.adopted {
		if _, ok := s.started[p]; !ok {
			loose = append(loose, p)
		}
	}
	dones := make([]chan struct{}, 0, len(s.started))
	for _, d := range s.started {
		dones = append(dones, d)
	}
	s.mu.Unlock()

	for _, p := range loose {
		terminate(p, false)
	}
	for _, p := range groups {
		terminate(p, true)
	}
	deadline := time.After(3 * time.Second)
	for _, d := range dones {
		select {
		case <-d:
		case <-deadline:
		}
	}
	for _, p := range loose {
		forceKill(p, false)
	}
	for _, p := range groups {
		forceKill(p, true)
	}
	for _, d := range dones {
		select {
		case <-d:
		case <-time.After(2 * time.Second):
		}
	}
}

// Alive reports whether pid still exists (a reaped or never-started PID is not).
func Alive(pid int) bool { return pidAlive(pid) }
