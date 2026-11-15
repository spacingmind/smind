//go:build windows

package runsock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows form of the per-run socket: a NAMED PIPE per run, created with a
// protected DACL that grants access to the daemon's user SID only, the first
// instance flagged so nobody can squat the name, remote clients rejected. On each
// connection the client's pid (GetNamedPipeClientProcessId) goes through the same
// decision as on unix (checkCred): a process of the run's tree, unchanged root.
//
// This file is written for Windows and compile-checked from another platform. It
// has NOT been run on Windows in the lane that added it: its first real run is the
// Windows CI job and a manual run (see docs/plans/active/browser-for-agents-pr3b2.md).
// It deviates from the plan's Job Object membership check: the run's job handle is
// not plumbed to this package, so the tree is the same parent-chain walk as on unix
// (ProcTree over a toolhelp snapshot); a process whose parent exited is not found
// and is refused (fail closed), and a parent that started AFTER its child (a
// recycled pid) is refused. Stated residual: a same-user process can create
// another instance of the pipe's name (FILE_FLAG_FIRST_PIPE_INSTANCE protects only
// the first) and the child does not authenticate the server end: the same-user
// rebinding of the unix socket path, inherent to the same-uid model.

const maxPipeInstances = 8

// Hub owns the per-run pipes of a daemon.
type Hub struct {
	tree ProcTree
	sddl string

	mu     sync.Mutex
	runs   map[string]*pipeRun
	closed bool
}

type pipeRun struct {
	id        string
	name      string
	root      int
	rootStart uint64
	handle    Handler
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	mu    sync.Mutex
	conns map[*pipeConn]struct{}
}

// NewSystemHub builds the hub of this process. The directory argument is the
// unix socket directory and is not used on Windows (pipes live in a kernel namespace).
func NewSystemHub(_ string) (*Hub, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("runsock: the current user: %w", err)
	}
	sddl, err := pipeSDDL(u.User.Sid.String())
	if err != nil {
		return nil, err
	}
	return &Hub{tree: SystemProcTree(), sddl: sddl, runs: map[string]*pipeRun{}}, nil
}

func (h *Hub) securityAttributes() (*windows.SecurityAttributes, error) {
	sd, err := windows.SecurityDescriptorFromString(h.sddl)
	if err != nil {
		return nil, fmt.Errorf("runsock: security descriptor: %w", err)
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func (h *Hub) newInstance(name string, first bool) (windows.Handle, error) {
	sa, err := h.securityAttributes()
	if err != nil {
		return 0, err
	}
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT | windows.PIPE_REJECT_REMOTE_CLIENTS)
	hd, err := windows.CreateNamedPipe(n, flags, mode, maxPipeInstances, 64<<10, 64<<10, 0, sa)
	if err != nil {
		return 0, fmt.Errorf("runsock: create pipe: %w", err)
	}
	return hd, nil
}

// Open creates the run's pipe, bound to the agent process rooted at rootPID.
func (h *Hub) Open(runID string, rootPID int, handle Handler) (string, error) {
	if runID == "" || rootPID <= 1 || handle == nil {
		return "", errors.New("runsock: a run, a root process and a handler are required")
	}
	start, err := StartOf(h.tree, rootPID)
	if err != nil {
		return "", fmt.Errorf("runsock: the run's root process cannot be read: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", errors.New("runsock: the hub is closed")
	}
	if old := h.runs[runID]; old != nil {
		old.close()
		delete(h.runs, runID)
	}
	name := pipeName(runID)
	first, err := h.newInstance(name, true) // fails if the name is taken: no squatting
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	pr := &pipeRun{id: runID, name: name, root: rootPID, rootStart: start, handle: handle,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), conns: map[*pipeConn]struct{}{}}
	h.runs[runID] = pr
	go h.accept(pr, first)
	return name, nil
}

// Close ends a run's pipe and its connections.
func (h *Hub) Close(runID string) {
	h.mu.Lock()
	pr := h.runs[runID]
	delete(h.runs, runID)
	h.mu.Unlock()
	if pr != nil {
		pr.close()
	}
}

// CloseAll ends every pipe and refuses further Opens.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	h.closed = true
	all := h.runs
	h.runs = map[string]*pipeRun{}
	h.mu.Unlock()
	for _, pr := range all {
		pr.close()
	}
}

func (pr *pipeRun) close() {
	pr.cancel()
	// Wake the accept loop blocked in ConnectNamedPipe by connecting to ourselves.
	if c, err := dialPipe(pr.name, 500*time.Millisecond); err == nil {
		_ = c.Close()
	}
	// Close the connections OUTSIDE the lock: a close can wait on a pending read.
	pr.mu.Lock()
	conns := make([]*pipeConn, 0, len(pr.conns))
	for c := range pr.conns {
		conns = append(conns, c)
	}
	pr.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	select {
	case <-pr.done:
	case <-time.After(3 * time.Second): // never hang the daemon on a stuck pipe
	}
}

func (h *Hub) accept(pr *pipeRun, inst windows.Handle) {
	defer close(pr.done)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		err := windows.ConnectNamedPipe(inst, nil)
		if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			_ = windows.CloseHandle(inst)
			return
		}
		if pr.ctx.Err() != nil {
			_ = windows.DisconnectNamedPipe(inst)
			_ = windows.CloseHandle(inst)
			return
		}
		conn := &pipeConn{h: inst, server: true}
		next, nerr := h.newInstance(pr.name, false)
		reason := h.checkClient(pr, inst)
		pr.mu.Lock()
		if reason == "" && len(pr.conns) >= maxConnsPerRun {
			reason = "too many connections"
		}
		if reason == "" {
			pr.conns[conn] = struct{}{}
		}
		pr.mu.Unlock()
		if reason != "" {
			_ = conn.Close() // no reply
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					pr.mu.Lock()
					delete(pr.conns, conn)
					pr.mu.Unlock()
				}()
				serveConn(pr.ctx, conn, pr.handle)
			}()
		}
		if nerr != nil {
			return // no further instance: the pipe is gone
		}
		inst = next
	}
}

// checkClient reads the client's pid and runs the shared decision.
func (h *Hub) checkClient(pr *pipeRun, inst windows.Handle) string {
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(inst, &pid); err != nil || pid == 0 {
		return "peer credentials unreadable"
	}
	return checkCred(PeerCred{PID: int(pid)}, false, 0, h.tree, pr.root, pr.rootStart)
}

// pipeConn is a duplex pipe handle with the deadline behaviour serveConn needs: a
// deadline that expires closes the connection (which is what an idle or stalled
// peer gets anyway).
type pipeConn struct {
	h      windows.Handle
	server bool

	once   sync.Once
	mu     sync.Mutex
	rtimer *time.Timer
	wtimer *time.Timer
}

func (c *pipeConn) Read(b []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(c.h, b, &n, nil)
	if err != nil {
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return int(n), io.EOF
		}
		if n == 0 {
			return 0, err
		}
	}
	return int(n), nil
}

func (c *pipeConn) Write(b []byte) (int, error) {
	total := 0
	for total < len(b) {
		var n uint32
		if err := windows.WriteFile(c.h, b[total:], &n, nil); err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}

func (c *pipeConn) Close() error {
	c.once.Do(func() {
		// Cancel any read another goroutine is blocked in (CancelIoEx reaches
		// synchronous I/O of any thread), so the disconnect and close cannot wait
		// on it. No FlushFileBuffers: it waits for the peer to read.
		_ = windows.CancelIoEx(c.h, nil)
		if c.server {
			_ = windows.DisconnectNamedPipe(c.h)
		}
		_ = windows.CloseHandle(c.h)
	})
	return nil
}

func (c *pipeConn) setTimer(slot **time.Timer, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot).Stop()
		*slot = nil
	}
	if !t.IsZero() {
		*slot = time.AfterFunc(time.Until(t), func() { _ = c.Close() })
	}
}

func (c *pipeConn) SetReadDeadline(t time.Time) error  { c.setTimer(&c.rtimer, t); return nil }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { c.setTimer(&c.wtimer, t); return nil }
func (c *pipeConn) SetDeadline(t time.Time) error {
	c.setTimer(&c.rtimer, t)
	c.setTimer(&c.wtimer, t)
	return nil
}

// dialPipe opens a run pipe as a client, retrying while every instance is busy.
func dialPipe(name string, wait time.Duration) (*pipeConn, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		h, err := windows.CreateFile(n, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			return &pipeConn{h: h}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) || time.Now().After(deadline) {
			return nil, &os.PathError{Op: "open", Path: name, Err: err}
		}
		time.Sleep(25 * time.Millisecond)
	}
}
