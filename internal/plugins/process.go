package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/store"
)

// Process hosting (ADR-0042 section 4, Wave B). The Manager owns one
// host.Host when Options.Host is set; a plugin that is enabled, whose consent
// is recorded and that declares `process` has a supervised process exactly
// while the global switch is on. Every path that changes what should run
// (enable, disable, reload, an applied update, remove, daemon start, a flip of
// the switch) reconciles it, and a failure of one plugin's host never reaches
// the daemon or another plugin.

// hostState is the Manager's process-hosting state.
type hostState struct {
	h        *host.Host
	onChange atomic.Pointer[func(string)]

	// red scrubs log lines: token shapes, home paths and every plugin's
	// current secret setting values.
	red     atomic.Pointer[redact.Redactor]
	homes   []string
	mu      sync.Mutex
	secrets map[string][]string
}

// newHost builds the host from o, forcing the global switch as its gate and
// routing its change notifications to the Manager.
func (m *Manager) newHost(o host.Options) {
	m.proc = &hostState{}
	o.Gate = m.GlobalEnabled
	o.OnChange = func(id string) {
		if f := m.proc.onChange.Load(); f != nil {
			(*f)(id)
		}
	}
	if o.Redact == nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			m.proc.homes = append(m.proc.homes, home)
		}
		m.proc.red.Store(redact.New(nil, m.proc.homes))
		o.Redact = func(s string) string { return m.proc.red.Load().String(s) }
	}
	m.proc.h = host.New(o)
}

// noteSecrets remembers a plugin's current secret values so every log line is
// scrubbed of them (a plugin that prints its own token to stderr, or a host
// message that quotes one, never stores it). Called whenever a spec is built.
func (m *Manager) noteSecrets(id string, man *Manifest, settings map[string]any) {
	if m.proc == nil || man == nil {
		return
	}
	var vals []string
	for _, s := range man.Settings {
		if s.Type != "secret" {
			continue
		}
		if v, ok := settings[s.Key].(string); ok && v != "" {
			vals = append(vals, v)
		}
	}
	m.proc.mu.Lock()
	defer m.proc.mu.Unlock()
	if m.proc.secrets == nil {
		m.proc.secrets = map[string][]string{}
	}
	m.proc.secrets[id] = vals
	var all []string
	for _, v := range m.proc.secrets {
		all = append(all, v...)
	}
	m.proc.red.Store(redact.New(all, m.proc.homes))
}

// SetOnChange registers the function told (asynchronously) that a plugin's
// process status moved: crash, restart, running, failed. The daemon wires it
// to the plugin.updated event.
func (m *Manager) SetOnChange(f func(id string)) {
	if m.proc != nil {
		m.proc.onChange.Store(&f)
	}
}

// hostSpec is the process spec of a plugin whose manifest declares one.
func (m *Manager) hostSpec(p store.Plugin, man *Manifest) (host.Spec, bool) {
	if man == nil || man.Process == nil {
		return host.Spec{}, false
	}
	dir := m.dir(p.ID)
	if p.SourceKind == string(SourceLink) {
		dir = p.Source
	}
	settings := m.resolved(p.ID, man)
	m.noteSecrets(p.ID, man, settings)
	return host.Spec{
		ID: p.ID, Version: man.Version, Dir: dir, DataDir: filepath.Join(m.dir(p.ID), "data"),
		Command: append([]string(nil), man.Process.Command...), DaemonVersion: m.version,
		Capabilities: append([]string(nil), man.Capabilities...),
		Settings:     settings, // decrypted: this goes to the plugin's initialize and nowhere else
	}, true
}

// syncOne makes the host match one plugin: stopped unless it is enabled, the
// global switch is on and its manifest declares a process; otherwise started
// (restart: replaced even when nothing about the spec changed). Callers hold
// m.mu. Stop blocks until the process tree is gone; start does not wait.
func (m *Manager) syncOne(p store.Plugin, man *Manifest, restart bool) {
	if m.proc == nil {
		return
	}
	spec, ok := m.hostSpec(p, man)
	if !ok || !p.Enabled || !m.GlobalEnabled() {
		m.proc.h.Stop(p.ID)
		return
	}
	if restart {
		m.proc.h.Restart(spec)
	} else {
		m.proc.h.Ensure(spec)
	}
}

// SyncHost reconciles every plugin with the host: the daemon calls it when the
// global switch flips (processes stop when it goes off and start when it goes
// on).
func (m *Manager) SyncHost() {
	if m.proc == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.st.ListPlugins()
	if err != nil {
		slog.Warn("plugins: sync host", "err", err)
		return
	}
	global := m.GlobalEnabled()
	for _, p := range rows {
		var man *Manifest
		if p.Enabled && global {
			man = m.loadedManifest(m.infoG(p, global))
		}
		m.syncOne(p, man, false)
	}
}

// restartAfterSwap restarts a plugin's process on the files an applied update
// just put in place.
func (m *Manager) restartAfterSwap(id string) {
	if m.proc == nil {
		return
	}
	p, err := m.st.GetPlugin(id)
	if err != nil {
		return
	}
	man, _, err := m.loadInstalled(p)
	if err != nil {
		m.setError(p, err)
		return
	}
	m.syncOne(p, man, true)
}

// Shutdown stops every plugin process and waits until each process group is
// gone. The daemon calls it once on its way down; it is safe to call twice.
func (m *Manager) Shutdown() {
	if m.proc != nil {
		m.proc.h.Close()
	}
}

// LogsResult is plugin.logs' result.
type LogsResult struct {
	Lines []host.LogEntry `json:"lines"`
}

// Default and maximum number of log lines one read returns.
const (
	defaultLogLines = 200
	maxLogLines     = 1000
)

// Logs returns a plugin's newest log lines, oldest first: what its process
// printed on stderr and the host's own `[smind]` messages, already redacted
// when stored. A plugin that never ran has none; one that was stopped keeps
// its lines until the daemon restarts or the plugin is removed.
func (m *Manager) Logs(id string, limit int) (LogsResult, error) {
	if _, err := m.row(id); err != nil {
		return LogsResult{}, err
	}
	if limit <= 0 {
		limit = defaultLogLines
	}
	if limit > maxLogLines {
		limit = maxLogLines
	}
	res := LogsResult{Lines: []host.LogEntry{}}
	if m.proc != nil {
		if ls := m.proc.h.Logs(id, limit); ls != nil {
			res.Lines = ls
		}
	}
	return res, nil
}

// ProcessStatus is a plugin's process status, false when none is supervised.
func (m *Manager) ProcessStatus(id string) (host.Status, bool) {
	if m.proc == nil {
		return host.Status{}, false
	}
	return m.proc.h.Status(id)
}

// CallProcess sends a host-to-plugin request to a plugin's running process and
// returns its raw answer. It is the Go entry point plugin.call (PB.4) will use
// and what tests drive a helper plugin with; no RPC exposes it yet.
func (m *Manager) CallProcess(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	if m.proc == nil {
		return nil, errors.New("process hosting is off")
	}
	return m.proc.h.Call(ctx, id, method, params)
}
