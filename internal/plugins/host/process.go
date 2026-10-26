package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/shimguard"
)

// Process is one running plugin process and its connection.
type Process struct {
	spec Spec
	cmd  *exec.Cmd
	pg   *processGroup
	conn *conn
	log  *Log
	pol  Policy
	errw *lineWriter

	stdin *os.File

	done    chan struct{}
	exitErr error

	stopOnce sync.Once
}

// resolve turns the manifest command into an executable path and arguments
// WITHOUT a shell: arguments are passed verbatim, so `;`, `$(...)` and quotes
// are just characters.
func resolve(spec Spec, environ []string) (string, []string, error) {
	if len(spec.Command) == 0 || spec.Command[0] == "" {
		return "", nil, errors.New("the manifest declares an empty process command")
	}
	prog, args := spec.Command[0], spec.Command[1:]
	if strings.ContainsAny(prog, `/\`) || filepath.IsAbs(prog) {
		p := filepath.FromSlash(prog)
		if !filepath.IsAbs(p) {
			p = filepath.Join(spec.Dir, p)
			rel, err := filepath.Rel(spec.Dir, p)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", nil, fmt.Errorf("process command %q leaves the plugin directory", prog)
			}
		}
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			return "", nil, fmt.Errorf("process program %q not found in the plugin", prog)
		}
		return p, args, nil
	}
	p, err := lookPath(prog, safePath(environ))
	if err != nil {
		return "", nil, fmt.Errorf("program %q not found on the plugin PATH (%s)", prog, safePath(environ))
	}
	return p, args, nil
}

// startProcess spawns the plugin and starts reading its stdout. It does not
// wait for the handshake (see initialize).
func startProcess(spec Spec, environ []string, pol Policy, reg Registry, log *Log) (*Process, error) {
	path, args, err := resolve(spec, environ)
	if err != nil {
		return nil, err
	}
	if err := shimguard.Validate("plugin", append([]string{path}, args...)); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(spec.DataDir, "tmp"), 0o700); err != nil {
		return nil, fmt.Errorf("create the plugin data directory: %w", err)
	}
	// Our own pipes, handed to the child as *os.File: Wait then has no copy
	// goroutine to wait for, so a grandchild holding stdout cannot stall it.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	p := &Process{spec: spec, log: log, pol: pol, errw: log.writer(), stdin: inW, done: make(chan struct{})}
	cmd := exec.Command(path, args...)
	cmd.Dir = spec.Dir
	cmd.Env = scrubbedEnv(environ, spec.DataDir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, p.errw
	cmd.WaitDelay = 2 * time.Second
	setProcessGroup(cmd)
	closeAll := func() { inR.Close(); inW.Close(); outR.Close(); outW.Close() }
	if err := cmd.Start(); err != nil {
		closeAll()
		return nil, fmt.Errorf("start %q: %w", spec.Command[0], err)
	}
	inR.Close()
	outW.Close()
	pg, err := attachProcessGroup(cmd)
	if err != nil {
		closeAll()
		return nil, err
	}
	p.cmd, p.pg = cmd, pg
	p.conn = newConn(spec.ID, outR, inW, pol.Limits, reg, log, pol.WriteTimeout, func(error) { p.pg.kill() })
	if so, ok := reg.(SessionOpener); ok {
		sess := so.Open(Plugin{ID: spec.ID, Capabilities: append([]string(nil), spec.Capabilities...)}, p.conn.notifyPeer)
		p.conn.reg, p.conn.onClose = sessionRegistry{sess}, sess.Close
	}
	go p.conn.readLoop()
	go func() {
		err := cmd.Wait()
		p.errw.Flush()
		p.exitErr = err
		// Let the reader report what the process left in the pipe (a truncated
		// frame is a protocol error, not a clean exit) before closing it.
		select {
		case <-p.conn.rdone:
		case <-time.After(500 * time.Millisecond):
		}
		p.conn.end(nil)
		outR.Close()
		inW.Close()
		close(p.done)
	}()
	log.Hostf("started %s (pid %d)", filepath.Base(path), cmd.Process.Pid)
	return p, nil
}

// PID is the leader's process id.
func (p *Process) PID() int { return p.cmd.Process.Pid }

// Done is closed when the process has exited and been reaped.
func (p *Process) Done() <-chan struct{} { return p.done }

// exitReason describes how the process ended (valid after Done).
func (p *Process) exitReason() string {
	if err := p.conn.Err(); err != nil && !errors.Is(err, ErrClosed) {
		return err.Error()
	}
	if p.exitErr != nil {
		return "exited: " + p.exitErr.Error()
	}
	return "exited with status 0"
}

type initializeParams struct {
	API          int            `json:"api"`
	SmindVersion string         `json:"smindVersion"`
	DataDir      string         `json:"dataDir"`
	Settings     map[string]any `json:"settings"`
}

// initialize is the start handshake: plugin.initialize, answered within the
// start timeout, is "ready". A plugin that errors, times out or dies is a
// failed start.
func (p *Process) initialize(ctx context.Context) error {
	settings := p.spec.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	_, err := p.conn.call(ctx, "plugin.initialize", initializeParams{
		API: APIVersion, SmindVersion: p.spec.DaemonVersion, DataDir: p.spec.DataDir, Settings: settings,
	}, p.pol.StartTimeout)
	switch {
	case err == nil:
		p.log.Hostf("ready")
		return nil
	case errors.Is(err, ErrTimeout):
		return fmt.Errorf("did not answer plugin.initialize within %s", p.pol.StartTimeout)
	}
	if errors.Is(err, ErrClosed) {
		select { // the pipe closed: give the reaper a moment to say why
		case <-p.done:
		case <-time.After(2 * time.Second):
		}
	}
	select {
	case <-p.done:
		return fmt.Errorf("%s before it answered plugin.initialize", p.exitReason())
	default:
	}
	return fmt.Errorf("plugin.initialize failed: %w", err)
}

// Call sends a host-to-plugin request (default request timeout).
func (p *Process) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.conn.call(ctx, method, params, p.pol.RequestTimeout)
}

// Notify sends a host-to-plugin notification (best effort: no answer, and a
// plugin that stopped reading is ended by the write timeout).
func (p *Process) Notify(method string, params any) error {
	return p.conn.notifyPeer(method, params)
}

// Stop ends the process and everything it spawned: a plugin.shutdown
// notification, SIGTERM to the process group, StopGrace, then SIGKILL to the
// group. The group is killed even when the leader exited by itself, so a
// grandchild never outlives its plugin. It returns once the leader is reaped.
func (p *Process) Stop() {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
		default:
			_ = p.conn.notifyPeer("plugin.shutdown", map[string]any{})
			p.pg.term()
			_ = p.stdin.Close() // EOF on stdin is the Windows graceful stop
			select {
			case <-p.done:
			case <-time.After(p.pol.StopGrace):
				p.log.Hostf("did not stop within %s: killing it", p.pol.StopGrace)
			}
		}
		p.pg.kill()
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			p.log.Hostf("the process did not exit after the kill")
		}
		p.pg.release()
	})
}
