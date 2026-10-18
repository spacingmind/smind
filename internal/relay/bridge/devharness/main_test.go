package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type proc struct {
	t     *testing.T
	stdin io.WriteCloser
	out   *bufio.Scanner
	cmd   *exec.Cmd
}

func (p *proc) line() string {
	p.t.Helper()
	type res struct {
		s  string
		ok bool
	}
	ch := make(chan res, 1)
	go func() { ok := p.out.Scan(); ch <- res{p.out.Text(), ok} }()
	select {
	case r := <-ch:
		if !r.ok {
			p.t.Fatalf("stdout closed: %v", p.out.Err())
		}
		return r.s
	case <-time.After(60 * time.Second):
		p.t.Fatal("timed out waiting for a stdout line")
		return ""
	}
}

// do sends one command and returns the decoded reply, asserting the cmd echo.
func (p *proc) do(cmd string) map[string]any {
	p.t.Helper()
	if _, err := io.WriteString(p.stdin, cmd+"\n"); err != nil {
		p.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.line()), &m); err != nil {
		p.t.Fatalf("%s: bad JSON reply: %v", cmd, err)
	}
	if m["cmd"] != strings.Fields(cmd)[0] {
		p.t.Fatalf("%s: reply cmd = %v", cmd, m["cmd"])
	}
	return m
}

func (p *proc) mustOK(cmd string) map[string]any {
	p.t.Helper()
	m := p.do(cmd)
	if m["ok"] != true {
		p.t.Fatalf("%s: %v", cmd, m)
	}
	return m
}

func (p *proc) state(label string) string {
	p.t.Helper()
	for _, d := range p.mustOK("list")["devices"].([]any) {
		dm := d.(map[string]any)
		if dm["label"] == label {
			return dm["state"].(string)
		}
	}
	return ""
}

func start(t *testing.T) *proc {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devharness")
	if runtime.GOOS == "windows" {
		bin += ".exe" // exec.Command does not append it to a path
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &proc{t: t, stdin: stdin, out: bufio.NewScanner(stdout), cmd: cmd}
	p.out.Buffer(make([]byte, 1<<20), 1<<20)
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			cancel()
			<-done
		}
		cancel()
	})
	if l := p.line(); !strings.HasPrefix(l, "READY ") {
		t.Fatalf("first line = %q", l)
	}
	if l := p.line(); !strings.HasPrefix(l, "NATIVE 127.0.0.1:") {
		t.Fatalf("second line = %q", l)
	}
	return p
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDeviceHarness(t *testing.T) {
	p := start(t)

	// Window closed: an unknown device is recorded pending and refused.
	if m := p.mustOK("peer-add a"); m["state"] != "awaiting_approval" {
		t.Fatalf("peer-add a: %v", m)
	}
	if s := p.state("a"); s != "pending" {
		t.Fatalf("a state = %q", s)
	}
	// Approve, then the same device (same persistent key) is admitted.
	if m := p.mustOK("approve a"); m["state"] != "approved" {
		t.Fatalf("approve a: %v", m)
	}
	if m := p.mustOK("peer-add a"); m["state"] != "approved" {
		t.Fatalf("peer-add a again: %v", m)
	}
	if m := p.mustOK("peer-call a"); m["result_len"] == nil {
		t.Fatalf("peer-call a: %v", m)
	}
	eventually(t, "a connected", func() bool {
		for _, d := range p.mustOK("list")["devices"].([]any) {
			if d.(map[string]any)["connected"] == true {
				return true
			}
		}
		return false
	})

	// A window of one auto-approves the next new device only.
	p.mustOK("window 1")
	if m := p.mustOK("peer-add b"); m["state"] != "approved" {
		t.Fatalf("peer-add b: %v", m)
	}
	if m := p.mustOK("peer-add x"); m["state"] != "awaiting_approval" {
		t.Fatalf("peer-add x (window spent): %v", m)
	}
	p.mustOK("peer-call b")
	p.mustOK("peer-call a") // a is unaffected by b

	// Revoke ends b's session; a keeps being served.
	if m := p.mustOK("revoke b"); m["state"] != "revoked" {
		t.Fatalf("revoke b: %v", m)
	}
	if m := p.do("peer-call b"); m["ok"] != false {
		t.Fatalf("peer-call b after revoke should fail: %v", m)
	}
	p.mustOK("peer-call a")
	if m := p.mustOK("peer-add b"); m["state"] != "revoked" {
		t.Fatalf("revoked b reconnecting: %v", m)
	}

	// peer-drop kills only the transport: the device stays approved.
	p.mustOK("peer-drop a")
	if s := p.state("a"); s != "approved" {
		t.Fatalf("a after drop = %q", s)
	}

	// Daemon restart (counters lost, registry kept): a fresh device still pairs.
	p.mustOK("daemon-restart")
	p.mustOK("window 1")
	if m := p.mustOK("peer-add c"); m["state"] != "approved" {
		t.Fatalf("peer-add c after daemon-restart: %v", m)
	}
	p.mustOK("peer-call c")
	if s := p.state("b"); s != "revoked" {
		t.Fatalf("b after daemon-restart = %q", s)
	}

	// Relay restart: the daemon reconnects and a new device is served again.
	p.mustOK("relay-restart")
	p.mustOK("window 1")
	eventually(t, "d approved through the restarted relay", func() bool {
		m := p.do("peer-add d")
		return m["ok"] == true && m["state"] == "approved"
	})
	p.mustOK("peer-call d")

	// Errors are reported, never crash.
	if m := p.do("approve nosuchdevice"); m["ok"] != false {
		t.Fatalf("approve unknown: %v", m)
	}
	if m := p.do("bogus"); m["ok"] != false {
		t.Fatalf("bogus: %v", m)
	}
	p.mustOK("quit")
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("exit: %v", err)
	}
}
