package workspace

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/spacingmind/smind/internal/store"
)

// SetWorktreesConfig applies the config.yaml `worktrees:` block (port range,
// setup/teardown timeouts). Nil-safe like SetNotifier.
func (m *Manager) SetWorktreesConfig(c WorktreesSettings) {
	if m == nil {
		return
	}
	m.wtMu.Lock()
	m.wt = c
	m.wtMu.Unlock()
}

func (m *Manager) worktreesSettings() WorktreesSettings {
	m.wtMu.Lock()
	defer m.wtMu.Unlock()
	return m.wt
}

// probePort reports whether 127.0.0.1:port can currently be bound. A Manager
// may override it (portProbe) for tests of the allocation logic.
func probePort(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// ErrPortsExhausted is returned when no port in the configured range is free.
var ErrPortsExhausted = errors.New("no free port in worktrees.ports.range")

// pickPort returns the lowest port in [lo, hi] that is not live and passes
// probe, or ErrPortsExhausted.
func pickPort(lo, hi int, live map[int]bool, probe func(int) bool) (int, error) {
	for p := lo; p <= hi; p++ {
		if live[p] || !probe(p) {
			continue
		}
		return p, nil
	}
	return 0, fmt.Errorf("%w (%d-%d)", ErrPortsExhausted, lo, hi)
}

// claimPort allocates the lowest free port in the configured range and
// records it on task id (ADR-0024). It returns 0 with no error when no range
// is configured. Allocation is serialised by a mutex; a unique-index conflict
// (another claimant outside the mutex) retries with a fresh live set.
func (m *Manager) claimPort(taskID int64) (int, error) {
	cfg := m.worktreesSettings()
	if !cfg.HasPorts {
		return 0, nil
	}
	m.portMu.Lock()
	defer m.portMu.Unlock()
	for attempt := 0; attempt < 5; attempt++ {
		live, err := m.store.ListLivePorts()
		if err != nil {
			return 0, err
		}
		probe := m.portProbe
		if probe == nil {
			probe = probePort
		}
		port, err := pickPort(cfg.PortLo, cfg.PortHi, live, probe)
		if err != nil {
			return 0, err
		}
		err = m.store.ClaimTaskPort(taskID, port)
		if err == nil {
			return port, nil
		}
		if !errors.Is(err, store.ErrPortTaken) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("claim port for task %d: too many conflicts", taskID)
}
