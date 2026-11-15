//go:build !windows

package runsock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const (
	// maxSockPath keeps a unix socket path under the smallest platform limit
	// (104 bytes on macOS, 108 on Linux) with room to spare.
	maxSockPath = 100
	// maxConnsPerRun bounds the live connections of one run's socket.
	maxConnsPerRun = 8
	sockSuffix     = ".sock"
)

// HubConfig configures a Hub.
type HubConfig struct {
	// Dir holds the sockets; it is created 0700 and verified (a directory, not a
	// symlink, owned by UID, mode 0700) or the Hub is refused.
	Dir string
	// FallbackDir is used when a socket path under Dir would be too long
	// (default "/tmp/smind-<uid>").
	FallbackDir string
	// UID is the daemon's user id: the only user a peer may be.
	UID int
	// Peers reads a connection's peer credentials; Tree reads process
	// information. Both are interfaces so a test can fake them.
	Peers PeerReader
	Tree  ProcTree
	// Lstat is os.Lstat (a test hook for the owner check).
	Lstat func(string) (fs.FileInfo, error)
	// OnRefuse is told about each refused connection with a FIXED reason (never a
	// pid or a path).
	OnRefuse func(runID, reason string)
}

// Hub owns the per-run sockets of a daemon.
type Hub struct {
	cfg HubConfig

	mu       sync.Mutex
	runs     map[string]*runSock
	closed   bool
	fallback string // the verified fallback directory, "" until first needed
}

type runSock struct {
	id        string
	path      string
	ln        net.Listener
	root      int
	rootStart uint64
	handle    Handler
	ctx       context.Context
	cancel    context.CancelFunc

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  chan struct{}
}

// NewHub prepares the socket directory (refusing a wrong owner, mode or type),
// and removes sockets a crashed daemon left behind: no run survives a restart.
func NewHub(cfg HubConfig) (*Hub, error) {
	if cfg.Peers == nil || cfg.Tree == nil {
		return nil, errors.New("runsock: a peer reader and a process tree are required")
	}
	if cfg.Lstat == nil {
		cfg.Lstat = os.Lstat
	}
	if cfg.FallbackDir == "" {
		cfg.FallbackDir = filepath.Join("/tmp", fmt.Sprintf("smind-%d", cfg.UID))
	}
	if err := prepareDir(cfg.Dir, cfg.UID, cfg.Lstat); err != nil {
		return nil, err
	}
	sweepStale(cfg.Dir)
	return &Hub{cfg: cfg, runs: map[string]*runSock{}}, nil
}

// NewSystemHub is NewHub with the platform's readers and the current user. It
// fails where the platform cannot read a peer's credentials, so a platform
// without them delivers no browser at all rather than a weaker check.
func NewSystemHub(dir string) (*Hub, error) {
	if !platformSupported {
		return nil, ErrPeerUnsupported
	}
	return NewHub(HubConfig{Dir: dir, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
}

// prepareDir creates dir (0700) and verifies it: a real directory, owned by uid,
// mode exactly 0700. Anything else is refused, never repaired: a directory that
// somebody else could write to or swap is not a place for a credential-less socket.
func prepareDir(dir string, uid int, lstat func(string) (fs.FileInfo, error)) error {
	if dir == "" {
		return errors.New("runsock: no socket directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("runsock: socket directory: %w", err)
	}
	info, err := lstat(dir)
	if err != nil {
		return fmt.Errorf("runsock: socket directory: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("runsock: %s is not a plain directory", dir)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("runsock: %s has mode %o, want 0700", dir, info.Mode().Perm())
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
		return fmt.Errorf("runsock: %s is owned by another user", dir)
	}
	return nil
}

// sweepStale removes leftover sockets: only files this package names (*.sock).
func sweepStale(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), sockSuffix) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}

func sockName(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(sum[:6]) + sockSuffix
}

// socketPath picks the directory for runID's socket: Dir, or the fallback when
// the path would be too long, or an error when neither fits.
func (h *Hub) socketPath(runID string) (string, error) {
	name := sockName(runID)
	if p := filepath.Join(h.cfg.Dir, name); len(p) <= maxSockPath {
		return p, nil
	}
	if h.fallback == "" {
		if err := prepareDir(h.cfg.FallbackDir, h.cfg.UID, h.cfg.Lstat); err != nil {
			return "", err
		}
		sweepStale(h.cfg.FallbackDir)
		h.fallback = h.cfg.FallbackDir
	}
	if p := filepath.Join(h.fallback, name); len(p) <= maxSockPath {
		return p, nil
	}
	return "", errors.New("runsock: no socket path short enough")
}

// Open creates the socket of a run, bound to the agent process rooted at
// rootPID (whose start time is recorded now), and returns its path. A second
// Open for the same run replaces the first.
func (h *Hub) Open(runID string, rootPID int, handle Handler) (string, error) {
	if runID == "" || rootPID <= 1 || handle == nil {
		return "", errors.New("runsock: a run, a root process and a handler are required")
	}
	start, err := StartOf(h.cfg.Tree, rootPID)
	if err != nil {
		return "", fmt.Errorf("runsock: the run's root process cannot be read: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", errors.New("runsock: the hub is closed")
	}
	path, err := h.socketPath(runID)
	if err != nil {
		return "", err
	}
	if old := h.runs[runID]; old != nil {
		old.close()
		delete(h.runs, runID)
	}
	_ = os.Remove(path) // our own leftover (the directory is ours alone)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", fmt.Errorf("runsock: listen: %w", err)
	}
	fail := func(e error) (string, error) {
		_ = ln.Close()
		_ = os.Remove(path)
		return "", e
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fail(fmt.Errorf("runsock: socket mode: %w", err))
	}
	if info, err := h.cfg.Lstat(path); err != nil || info.Mode()&fs.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		return fail(errors.New("runsock: the socket is not a 0600 socket"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	rs := &runSock{id: runID, path: path, ln: ln, root: rootPID, rootStart: start, handle: handle,
		ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{}, done: make(chan struct{})}
	h.runs[runID] = rs
	go h.accept(rs)
	return path, nil
}

// Close closes a run's socket, its connections and removes the file.
func (h *Hub) Close(runID string) {
	h.mu.Lock()
	rs := h.runs[runID]
	delete(h.runs, runID)
	h.mu.Unlock()
	if rs != nil {
		rs.close()
	}
}

// CloseAll closes every socket and refuses further Opens (daemon shutdown).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	h.closed = true
	all := h.runs
	h.runs = map[string]*runSock{}
	h.mu.Unlock()
	for _, rs := range all {
		rs.close()
	}
}

func (rs *runSock) close() {
	rs.cancel()
	_ = rs.ln.Close()
	_ = os.Remove(rs.path)
	rs.mu.Lock()
	for c := range rs.conns {
		_ = c.Close()
	}
	rs.mu.Unlock()
	<-rs.done
}

func (h *Hub) refuse(rs *runSock, conn net.Conn, reason string) {
	if h.cfg.OnRefuse != nil {
		h.cfg.OnRefuse(rs.id, reason)
	}
	_ = conn.Close() // no reply: a refused peer learns nothing
}

func (h *Hub) accept(rs *runSock) {
	defer close(rs.done)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := rs.ln.Accept()
		if err != nil {
			return
		}
		rs.mu.Lock()
		if len(rs.conns) >= maxConnsPerRun || rs.ctx.Err() != nil {
			rs.mu.Unlock()
			h.refuse(rs, conn, "too many connections")
			continue
		}
		rs.conns[conn] = struct{}{}
		rs.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				rs.mu.Lock()
				delete(rs.conns, conn)
				rs.mu.Unlock()
			}()
			// The check, before a single byte is read.
			if reason := h.checkPeer(rs, conn); reason != "" {
				h.refuse(rs, conn, reason)
				return
			}
			serveConn(rs.ctx, conn, rs.handle)
		}()
	}
}

// checkPeer says why a connection must be refused ("" means allowed): the peer
// is the daemon's user AND a process of the run's tree (the run's root process
// itself, unchanged since the socket was created, or a descendant of it).
func (h *Hub) checkPeer(rs *runSock, conn net.Conn) string {
	pc, err := h.cfg.Peers.Peer(conn)
	if err != nil {
		return "peer credentials unreadable"
	}
	if int64(pc.UID) != int64(h.cfg.UID) {
		return "another user"
	}
	ok, err := InTree(h.cfg.Tree, pc.PID, rs.root, rs.rootStart)
	if err != nil || !ok {
		return "not a process of this run"
	}
	return ""
}
