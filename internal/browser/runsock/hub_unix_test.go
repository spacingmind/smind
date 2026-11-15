//go:build !windows

package runsock

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---- helper processes: the test binary re-executes itself as an "agent" (a
// root process) that starts a "client" child which dials a socket, so the tests
// run against REAL process trees and real kernel peer credentials.

func TestMain(m *testing.M) {
	switch os.Getenv("RUNSOCK_HELPER") {
	case "agent":
		helperAgent()
	case "client":
		helperClient()
	}
	os.Exit(m.Run())
}

// helperAgent waits for a line on stdin (the socket path), then starts the
// client child with it and relays the child's one output line.
func helperAgent() {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "RUNSOCK_HELPER=client", "RUNSOCK_SOCK="+strings.TrimSpace(line))
	out, err := cmd.Output()
	if err != nil {
		fmt.Printf("CLIENT-ERROR %v\n", err)
		os.Exit(0)
	}
	os.Stdout.Write(out)
	os.Exit(0)
}

// helperClient dials the socket, sends one request, prints the first reply line,
// or CLOSED if the connection ended with no reply.
func helperClient() {
	conn, err := net.Dial("unix", os.Getenv("RUNSOCK_SOCK"))
	if err != nil {
		fmt.Println("DIAL-ERROR")
		os.Exit(0)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	fmt.Fprintf(conn, "{\"tool\":\"browser_tabs\"}\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		fmt.Println("CLOSED")
		os.Exit(0)
	}
	fmt.Print(line)
	os.Exit(0)
}

// startAgent starts an agent helper (a root process of its own process group)
// and returns it with a function that gives it the socket path and returns what
// its client printed.
func startAgent(t *testing.T) (pid int, run func(path string) string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "RUNSOCK_HELPER=agent")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the whole group: the agent and its client child
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid, func(path string) string {
		fmt.Fprintf(in, "%s\n", path)
		b, _ := io.ReadAll(out)
		return strings.TrimSpace(string(b))
	}
}

// ---- fixtures

func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func realHub(t *testing.T) *Hub {
	t.Helper()
	h, err := NewHub(HubConfig{Dir: filepath.Join(shortTemp(t), "sock"), UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.CloseAll)
	return h
}

func echo(_ context.Context, req Request) (any, error) {
	return map[string]any{"tool": req.Tool}, nil
}

// refusals collects the hub's refusal records (safe for the server goroutines).
type refusals struct {
	mu   sync.Mutex
	list []string
}

func (r *refusals) add(reason string) {
	r.mu.Lock()
	r.list = append(r.list, reason)
	r.mu.Unlock()
}

func (r *refusals) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.list...)
}

// fakeHub builds a hub whose peer and tree answers the test controls.
func fakeHub(t *testing.T, peers PeerReader, tree ProcTree, refused *refusals) *Hub {
	t.Helper()
	h, err := NewHub(HubConfig{Dir: filepath.Join(shortTemp(t), "sock"), UID: os.Getuid(), Peers: peers, Tree: tree,
		OnRefuse: func(_ string, reason string) { refused.add(reason) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.CloseAll)
	return h
}

// roundTrip dials the path, sends line, and returns the first reply line ("" with
// closed=true when the connection ended with no reply).
func roundTrip(t *testing.T, path, line string) (reply string, closed bool) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	_, _ = io.WriteString(conn, line)
	r, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(r), false
}

// ---- real process trees

func TestServerAcceptsADescendant(t *testing.T) {
	h := realHub(t)
	pid, run := startAgent(t)
	path, err := h.Open("run-A", pid, echo)
	if err != nil {
		t.Fatal(err)
	}
	got := run(path)
	if got != `{"result":{"tool":"browser_tabs"}}` {
		t.Fatalf("the run's own child must be served: %q", got)
	}
}

func TestServerRefusesOutsideTheTree(t *testing.T) {
	h := realHub(t)
	pid, _ := startAgent(t) // a root exists, but THIS process is not under it
	path, err := h.Open("run-A", pid, echo)
	if err != nil {
		t.Fatal(err)
	}
	if reply, closed := roundTrip(t, path, "{\"tool\":\"browser_tabs\"}\n"); !closed {
		t.Fatalf("a process outside the run's tree must get no reply: %q", reply)
	}
}

func TestServerRefusesAnotherRunsTree(t *testing.T) {
	h := realHub(t)
	pidA, _ := startAgent(t)
	pidB, runB := startAgent(t)
	pathA, err := h.Open("run-A", pidA, echo)
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := h.Open("run-B", pidB, echo)
	if err != nil {
		t.Fatal(err)
	}
	if pathA == pathB {
		t.Fatal("each run has its own socket")
	}
	// B's own child reaching B's socket works; B's child dialling A's socket is refused.
	if got := runB(pathA); got != "CLOSED" {
		t.Fatalf("a process of run B must be refused on run A's socket: %q", got)
	}
	pidB2, runB2 := startAgent(t)
	pathB2, err := h.Open("run-B2", pidB2, echo)
	if err != nil {
		t.Fatal(err)
	}
	if got := runB2(pathB2); !strings.HasPrefix(got, `{"result"`) {
		t.Fatalf("its own socket serves it: %q", got)
	}
	_ = pathB
}

// ---- the check, table over fakes

func TestServerCheckTable(t *testing.T) {
	const root = 5000
	const rootStart = 77
	uid := uint32(os.Getuid())
	tree := func(rootStartNow uint64, peerParent int, peerErr error) ProcTree {
		return FuncProcTree(func(pid int) (ProcInfo, error) {
			switch pid {
			case root:
				return ProcInfo{PPID: 1, Start: rootStartNow}, nil
			case 6000:
				if peerErr != nil {
					return ProcInfo{}, peerErr
				}
				return ProcInfo{PPID: peerParent, Start: 80}, nil
			}
			return ProcInfo{}, ErrNoProcess
		})
	}
	cases := []struct {
		name      string
		peer      func(net.Conn) (PeerCred, error)
		tree      ProcTree
		wantServe bool
	}{
		{"right user, in tree", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid, PID: 6000}, nil }, tree(rootStart, root, nil), true},
		{"another user", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid + 1, PID: 6000}, nil }, tree(rootStart, root, nil), false},
		{"root user is not the daemon's user", func(net.Conn) (PeerCred, error) { return PeerCred{UID: 0, PID: 6000}, nil }, tree(rootStart, root, nil), uid == 0},
		{"credentials unreadable", func(net.Conn) (PeerCred, error) { return PeerCred{}, errors.New("no creds") }, tree(rootStart, root, nil), false},
		{"outside the tree", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid, PID: 6000}, nil }, tree(rootStart, 1, nil), false},
		{"the root was replaced (start changed)", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid, PID: 6000}, nil }, tree(rootStart+1, root, nil), false},
		{"the peer vanished", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid, PID: 6000}, nil }, tree(rootStart, root, ErrNoProcess), false},
		{"an unknown pid", func(net.Conn) (PeerCred, error) { return PeerCred{UID: uid, PID: 7777}, nil }, tree(rootStart, root, nil), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var refused refusals
			// The tree at Open time must show the recorded start; then it changes per case.
			var curMu sync.Mutex
			var cur ProcTree = tree(rootStart, root, nil)
			dyn := FuncProcTree(func(pid int) (ProcInfo, error) {
				curMu.Lock()
				t := cur
				curMu.Unlock()
				return t.Info(pid)
			})
			h := fakeHub(t, FuncPeerReader(c.peer), dyn, &refused)
			path, err := h.Open("run-A", root, echo)
			if err != nil {
				t.Fatal(err)
			}
			curMu.Lock()
			cur = c.tree
			curMu.Unlock()
			reply, closed := roundTrip(t, path, "{\"tool\":\"browser_tabs\"}\n")
			if c.wantServe == closed {
				t.Fatalf("served=%v closed=%v reply=%q refused=%v", !closed, closed, reply, refused.all())
			}
			if !c.wantServe && len(refused.all()) == 0 {
				t.Fatal("a refusal is recorded")
			}
			for _, r := range refused.all() {
				if strings.ContainsAny(r, "0123456789") {
					t.Fatalf("a refusal record carries no process data: %q", r)
				}
			}
		})
	}
}

func TestServerRefusesAnExitedPeer(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no 'true' to run")
	}
	dead := cmd.Process.Pid
	real := SystemProcTree()
	var refused refusals
	// The peer claims a pid that no longer exists.
	h := fakeHub(t, FuncPeerReader(func(net.Conn) (PeerCred, error) { return PeerCred{UID: uint32(os.Getuid()), PID: dead}, nil }), real, &refused)
	path, err := h.Open("run-A", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	if _, closed := roundTrip(t, path, "{\"tool\":\"browser_tabs\"}\n"); !closed {
		t.Fatal("an exited peer is refused")
	}
}

func TestOpenNeedsTheRootsStartTime(t *testing.T) {
	var refused refusals
	h := fakeHub(t, FuncPeerReader(func(net.Conn) (PeerCred, error) { return PeerCred{}, nil }), FuncProcTree(func(int) (ProcInfo, error) { return ProcInfo{}, ErrNoProcess }), &refused)
	if _, err := h.Open("run-A", 4242, echo); err == nil {
		t.Fatal("a root process that cannot be read means no socket")
	}
	for _, bad := range []int{0, 1, -3} {
		if _, err := h.Open("run-A", bad, echo); err == nil {
			t.Fatalf("root pid %d refused", bad)
		}
	}
	if _, err := h.Open("", 4242, echo); err == nil {
		t.Fatal("a run id is required")
	}
}

// ---- directory and file hygiene

func TestSocketDirAndFileModes(t *testing.T) {
	h := realHub(t)
	path, err := h.Open("run-A", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Lstat(filepath.Dir(path))
	if err != nil || !di.IsDir() || di.Mode().Perm() != 0o700 {
		t.Fatalf("the directory is 0700: %v %v", di, err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the socket is 0600: %v %v", fi, err)
	}
	if st := fi.Sys().(*syscall.Stat_t); int(st.Uid) != os.Getuid() {
		t.Fatal("the socket is ours")
	}
}

func TestSocketDirRefusedWhenWrongModeTypeOrOwner(t *testing.T) {
	base := shortTemp(t)
	mk := func(mode os.FileMode) string {
		d := filepath.Join(base, fmt.Sprintf("d%o", mode))
		if err := os.Mkdir(d, mode); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(d, mode)
		return d
	}
	for _, mode := range []os.FileMode{0o755, 0o770, 0o777, 0o500} {
		if _, err := NewHub(HubConfig{Dir: mk(mode), UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()}); err == nil {
			t.Errorf("a directory with mode %o must be refused", mode)
		}
	}
	// A symlink to a good directory.
	good := mk(0o700)
	link := filepath.Join(base, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHub(HubConfig{Dir: link, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()}); err == nil {
		t.Error("a symlinked directory must be refused")
	}
	// A regular file where the directory should be.
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHub(HubConfig{Dir: file, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()}); err == nil {
		t.Error("a file in place of the directory must be refused")
	}
	// Wrong owner, through the injected Lstat.
	wrongOwner := func(p string) (fs.FileInfo, error) {
		fi, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		return ownerFake{fi}, nil
	}
	if _, err := NewHub(HubConfig{Dir: good, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree(), Lstat: wrongOwner}); err == nil {
		t.Error("a directory owned by another user must be refused")
	}
	// And the good one is accepted.
	if _, err := NewHub(HubConfig{Dir: good, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()}); err != nil {
		t.Errorf("a 0700 directory of ours is fine: %v", err)
	}
}

type ownerFake struct{ fs.FileInfo }

func (o ownerFake) Sys() any {
	st := *(o.FileInfo.Sys().(*syscall.Stat_t))
	st.Uid++
	return &st
}

func TestSocketPathFallbackAndTooLong(t *testing.T) {
	base := shortTemp(t)
	long := filepath.Join(base, strings.Repeat("d", 90), "sock")
	fb := filepath.Join(shortTemp(t), "fb")
	h, err := NewHub(HubConfig{Dir: long, FallbackDir: fb, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.CloseAll)
	path, err := h.Open("run-A", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != fb || len(path) > maxSockPath {
		t.Fatalf("a long path falls back to the short directory: %s", path)
	}
	// Neither fits: no socket.
	h2, err := NewHub(HubConfig{Dir: long, FallbackDir: filepath.Join(base, strings.Repeat("f", 95)), UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h2.CloseAll)
	if _, err := h2.Open("run-B", os.Getpid(), echo); err == nil {
		t.Fatal("no path short enough: no socket")
	}
}

func TestStaleSocketsSweptAtStart(t *testing.T) {
	dir := filepath.Join(shortTemp(t), "sock")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "deadbeef0000.sock")
	other := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHub(HubConfig{Dir: dir, UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.CloseAll)
	if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a stale socket of a crashed daemon is swept")
	}
	if _, err := os.Lstat(other); err != nil {
		t.Fatal("files that are not sockets are left alone")
	}
}

func TestCloseRemovesListenerFileAndConnections(t *testing.T) {
	var refused refusals
	h := fakeHub(t, FuncPeerReader(func(net.Conn) (PeerCred, error) { return PeerCred{UID: uint32(os.Getuid()), PID: os.Getpid()}, nil }), SystemProcTree(), &refused)
	path, err := h.Open("run-A", os.Getpid(), func(ctx context.Context, req Request) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "{\"tool\":\"browser_tabs\"}\n")
	if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
		t.Fatalf("served before close: %v", err)
	}
	h.Close("run-A")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the socket file is removed at run end")
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
		t.Fatal("an open connection is closed at run end")
	}
	if _, err := net.Dial("unix", path); err == nil {
		t.Fatal("nothing listens after close")
	}
	// Reopen replaces; CloseAll refuses further opens.
	p1, err := h.Open("run-B", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := h.Open("run-B", os.Getpid(), echo)
	if err != nil || p1 != p2 {
		t.Fatalf("reopen: %v %s %s", err, p1, p2)
	}
	h.CloseAll()
	if _, err := h.Open("run-C", os.Getpid(), echo); err == nil {
		t.Fatal("a closed hub opens nothing")
	}
}

// ---- protocol

func peerOK() PeerReader {
	return FuncPeerReader(func(net.Conn) (PeerCred, error) { return PeerCred{UID: uint32(os.Getuid()), PID: os.Getpid()}, nil })
}

func protoHub(t *testing.T) (path string, calls *[]Request) {
	t.Helper()
	var refused refusals
	h := fakeHub(t, peerOK(), SystemProcTree(), &refused)
	var mu sync.Mutex
	var got []Request
	p, err := h.Open("run-A", os.Getpid(), func(_ context.Context, req Request) (any, error) {
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		if req.Tool == "boom" {
			return nil, errors.New("refused: not enabled")
		}
		return map[string]any{"tool": req.Tool, "n": len(req.Args)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, &got
}

func TestProtocolClosedSet(t *testing.T) {
	path, calls := protoHub(t)
	bad := map[string]string{
		"unknown field":       "{\"tool\":\"browser_tabs\",\"token\":\"x\"}\n",
		"method name":         "{\"method\":\"browser.call\",\"tool\":\"browser_tabs\"}\n",
		"run id":              "{\"runId\":\"other\",\"tool\":\"browser_tabs\"}\n",
		"bad json":            "{not json}\n",
		"no tool":             "{\"args\":{}}\n",
		"empty tool":          "{\"tool\":\"\"}\n",
		"huge tool name":      "{\"tool\":\"" + strings.Repeat("a", 65) + "\"}\n",
		"trailing data":       "{\"tool\":\"browser_tabs\"} {\"tool\":\"x\"}\n",
		"array":               "[\"browser_tabs\"]\n",
		"oversize line":       "{\"tool\":\"browser_open\",\"args\":{\"url\":\"" + strings.Repeat("a", 16<<10) + "\"}}\n",
		"oversize no newline": strings.Repeat("a", 16<<10+100),
	}
	for name, line := range bad {
		if reply, closed := roundTrip(t, path, line); !closed {
			t.Errorf("%s must close the connection with no reply: %q", name, reply)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a bad request never reaches the handler: %+v", *calls)
	}
	// The biggest legal request is served.
	pad := strings.Repeat("a", 16<<10-60)
	if reply, closed := roundTrip(t, path, "{\"tool\":\"browser_tabs\",\"args\":{\"p\":\""+pad+"\"}}\n"); closed || !strings.Contains(reply, `"result"`) {
		t.Fatalf("a request just under the cap is served: closed=%v %.80q", closed, reply)
	}
	// A handler error becomes an error reply, not a closed connection.
	if reply, closed := roundTrip(t, path, "{\"tool\":\"boom\"}\n"); closed || reply != `{"error":"refused: not enabled"}` {
		t.Fatalf("handler error: closed=%v %q", closed, reply)
	}
}

func TestProtocolSequentialRequestsAndSecondConnection(t *testing.T) {
	path, _ := protoHub(t)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	for i := 0; i < 3; i++ {
		fmt.Fprintf(conn, "{\"tool\":\"t%d\"}\n", i)
		line, err := br.ReadString('\n')
		var resp struct{ Result struct{ Tool string } }
		if err != nil || json.Unmarshal([]byte(line), &resp) != nil || resp.Result.Tool != fmt.Sprintf("t%d", i) {
			t.Fatalf("request %d: %q %v", i, line, err)
		}
	}
	// A second connection works at the same time.
	if reply, closed := roundTrip(t, path, "{\"tool\":\"other\"}\n"); closed || !strings.Contains(reply, "other") {
		t.Fatalf("second connection: %q", reply)
	}
}

func TestProtocolConnectionCap(t *testing.T) {
	path, _ := protoHub(t)
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	const cap = 8 // the documented per-run connection cap, not the constant under test
	for i := 0; i < cap; i++ {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		fmt.Fprintf(c, "{\"tool\":\"hold\"}\n")
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
	}
	if _, closed := roundTrip(t, path, "{\"tool\":\"one-too-many\"}\n"); !closed {
		t.Fatal("a connection beyond the cap is refused")
	}
}

func TestDeadlines(t *testing.T) {
	old := idleTimeout
	idleTimeout = 150 * time.Millisecond
	t.Cleanup(func() { idleTimeout = old })
	path, _ := protoHub(t)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
		t.Fatal("an idle connection is closed")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the idle deadline applies")
	}
}

// A handler that panics gets an error reply, the connection survives, and the
// daemon does not crash.
func TestHandlerPanicIsAnErrorReply(t *testing.T) {
	var refused refusals
	h := fakeHub(t, peerOK(), SystemProcTree(), &refused)
	path, err := h.Open("run-A", os.Getpid(), func(_ context.Context, req Request) (any, error) {
		if req.Tool == "panic" {
			panic("boom")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	fmt.Fprintf(conn, "{\"tool\":\"panic\"}\n")
	if line, err := br.ReadString('\n'); err != nil || strings.TrimSpace(line) != `{"error":"internal error"}` {
		t.Fatalf("a panic is an error reply: %q %v", line, err)
	}
	fmt.Fprintf(conn, "{\"tool\":\"fine\"}\n")
	if line, err := br.ReadString('\n'); err != nil || !strings.Contains(line, `"ok"`) {
		t.Fatalf("the connection survives: %q %v", line, err)
	}
}

// Refusals are counted per run (and the first is logged), so they are not silent.
func TestRefusalsAreCounted(t *testing.T) {
	var refused refusals
	h := fakeHub(t, FuncPeerReader(func(net.Conn) (PeerCred, error) { return PeerCred{}, errors.New("no creds") }), SystemProcTree(), &refused)
	path, err := h.Open("run-A", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		roundTrip(t, path, "{\"tool\":\"x\"}\n")
	}
	if got := h.Refusals("run-A"); got != 5 {
		t.Fatalf("refusals counted: %d", got)
	}
	if h.Refusals("unknown") != 0 {
		t.Fatal("an unknown run has none")
	}
}

// The default fallback directory is keyed on this daemon's own socket directory,
// so two daemons of one user never share (or sweep) a fallback.
func TestDefaultFallbackDirIsPerDaemon(t *testing.T) {
	mk := func() *Hub {
		h, err := NewHub(HubConfig{Dir: filepath.Join(shortTemp(t), "sock"), UID: os.Getuid(), Peers: SystemPeerReader(), Tree: SystemProcTree()})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	a, b := mk(), mk()
	if a.cfg.FallbackDir == b.cfg.FallbackDir || !strings.HasPrefix(a.cfg.FallbackDir, "/tmp/smind-") {
		t.Fatalf("fallback dirs: %q %q", a.cfg.FallbackDir, b.cfg.FallbackDir)
	}
}

// Replacing a run's socket closes the old one without holding up other runs.
func TestReopenReplacesWithoutBlockingOtherRuns(t *testing.T) {
	var refused refusals
	h := fakeHub(t, peerOK(), SystemProcTree(), &refused)
	started := make(chan struct{})
	release := make(chan struct{})
	pathA, err := h.Open("run-A", os.Getpid(), func(ctx context.Context, _ Request) (any, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "late", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { // not roundTrip: it may Fatal, and this is not the test goroutine
		if c, err := net.Dial("unix", pathA); err == nil {
			defer c.Close()
			_, _ = io.WriteString(c, "{\"tool\":\"slow\"}\n")
			_ = c.SetReadDeadline(time.Now().Add(4 * time.Second))
			_, _ = bufio.NewReader(c).ReadString('\n')
		}
	}()
	<-started
	done := make(chan error, 1)
	go func() { _, err := h.Open("run-A", os.Getpid(), echo); done <- err }()
	// Another run is not held up while run A's replacement waits for its busy handler.
	if _, err := h.Open("run-B", os.Getpid(), echo); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("the replacement must finish once the old handler's context is cancelled")
	}
}

// An unauthenticated flood must not use up the run's connection slots: the peer
// check comes before slot accounting, so with every pending check stuck (a slow
// reader) a legitimate client is still served.
func TestFloodOfUncheckedConnectionsDoesNotStarveTheLegitClient(t *testing.T) {
	const flood = 20 // more than the 8 connection slots, fewer than the pending cap
	release := make(chan struct{})
	var seen atomic.Int64
	peers := FuncPeerReader(func(net.Conn) (PeerCred, error) {
		if seen.Add(1) <= flood {
			<-release // the first connections' checks are slow
			return PeerCred{UID: uint32(os.Getuid()) + 1, PID: 1}, nil
		}
		return PeerCred{UID: uint32(os.Getuid()), PID: os.Getpid()}, nil
	})
	var refused refusals
	h := fakeHub(t, peers, SystemProcTree(), &refused)
	path, err := h.Open("run-A", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < flood; i++ {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	// Wait until the hub has accepted all of them (every check is pending).
	deadline := time.Now().Add(5 * time.Second)
	for seen.Load() < flood && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if seen.Load() < flood {
		t.Fatalf("the hub accepted %d of %d connections", seen.Load(), flood)
	}
	if reply, closed := roundTrip(t, path, "{\"tool\":\"legit\"}\n"); closed || !strings.Contains(reply, "legit") {
		t.Fatalf("a legitimate client during a flood: closed=%v %q", closed, reply)
	}
	close(release)
	for deadline := time.Now().Add(5 * time.Second); h.Refusals("run-A") < flood && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Refusals("run-A") != flood {
		t.Fatalf("every flood connection is refused: %d", h.Refusals("run-A"))
	}
}

// A second hub on the same directory must not remove a live daemon's sockets,
// only the ones nobody listens on.
func TestSweepKeepsLiveSocketsAndRemovesDeadOnes(t *testing.T) {
	dir := filepath.Join(shortTemp(t), "sock")
	var refused refusals
	h1, err := NewHub(HubConfig{Dir: dir, UID: os.Getuid(), Peers: peerOK(), Tree: SystemProcTree(), OnRefuse: func(_, r string) { refused.add(r) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h1.CloseAll)
	livePath, err := h1.Open("live-run", os.Getpid(), echo)
	if err != nil {
		t.Fatal(err)
	}
	// A dead socket: bound, then abandoned (the file stays, nobody accepts).
	dead := filepath.Join(dir, "deadbeef0000.sock")
	ln, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Lstat(dead); err != nil {
		t.Fatalf("test setup: the dead socket file exists: %v", err)
	}
	h2, err := NewHub(HubConfig{Dir: dir, UID: os.Getuid(), Peers: peerOK(), Tree: SystemProcTree()}) // sweeps
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h2.CloseAll)
	if _, err := os.Lstat(dead); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a socket nobody listens on is swept")
	}
	if reply, closed := roundTrip(t, livePath, "{\"tool\":\"still\"}\n"); closed || !strings.Contains(reply, "still") {
		t.Fatalf("a second hub must not break a live daemon's socket: closed=%v %q", closed, reply)
	}
}

// Closing a run's socket never waits forever for a handler that ignores its
// context: the run's end (and every other run's Open) is not stalled.
func TestCloseDoesNotWaitForeverForAStuckHandler(t *testing.T) {
	old := closeWait
	closeWait = 300 * time.Millisecond
	t.Cleanup(func() { closeWait = old })
	var refused refusals
	h := fakeHub(t, peerOK(), SystemProcTree(), &refused)
	stuck := make(chan struct{})
	started := make(chan struct{})
	path, err := h.Open("run-A", os.Getpid(), func(context.Context, Request) (any, error) {
		close(started)
		<-stuck // ignores ctx
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if c, err := net.Dial("unix", path); err == nil {
			defer c.Close()
			_, _ = io.WriteString(c, "{\"tool\":\"hang\"}\n")
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _ = bufio.NewReader(c).ReadString('\n')
		}
	}()
	<-started
	done := make(chan struct{})
	go func() { h.Close("run-A"); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		close(stuck)
		t.Fatal("Close waited for a handler that ignores its context")
	}
	close(stuck)
	if _, err := h.Open("run-B", os.Getpid(), echo); err != nil {
		t.Fatalf("another run opens meanwhile: %v", err)
	}
}

// A system hub is lazy: constructing it touches no disk, so a daemon or a test
// that never opens a socket never creates (or sweeps) anything in the real home.
func TestSystemHubIsLazyAndTouchesNoDiskUntilOpen(t *testing.T) {
	dir := filepath.Join(shortTemp(t), "never", "created")
	h, err := NewSystemHub(dir)
	if err != nil {
		t.Skip("no peer credentials here")
	}
	t.Cleanup(h.CloseAll)
	if _, err := os.Lstat(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructing a system hub must not create the directory")
	}
	if _, err := h.Open("run-A", os.Getpid(), echo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); err != nil {
		t.Fatalf("the first Open creates it: %v", err)
	}
}

func TestReadResponseIsBounded(t *testing.T) {
	big := strings.Repeat("a", MaxResponseBytes+10) + "\n"
	if _, err := ReadResponse(strings.NewReader(big)); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("an oversize reply: %v", err)
	}
	if _, err := ReadResponse(strings.NewReader(`{"result":1}` + "\n")); err != nil {
		t.Fatalf("a normal reply: %v", err)
	}
	if _, err := ReadResponse(strings.NewReader(`{"result":`)); err == nil {
		t.Fatal("a reply with no newline is an error")
	}
}
