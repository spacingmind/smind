// Package hosttest is the test-only plugin process: a test binary re-executes
// itself with `--plugin-helper <mode>` (see Main) and plays a plugin over the
// LSP-framed JSON-RPC stdio protocol, with its own independent framing code.
// It is imported by tests only; nothing in the daemon links it.
package hosttest

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Flag is the first argument that turns a test binary into a plugin.
const Flag = "--plugin-helper"

// Main runs the plugin and exits when os.Args[1] is Flag, and returns false
// otherwise (call it first in TestMain).
func Main() bool {
	if len(os.Args) < 3 || os.Args[1] != Flag {
		return false
	}
	run(os.Args[2], os.Args[3:])
	os.Exit(0)
	return true
}

// Command is the argv a manifest-less test uses to launch the helper in mode.
func Command(mode string, args ...string) []string {
	return append([]string{os.Args[0], Flag, mode}, args...)
}

type msg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

var (
	in  = bufio.NewReader(os.Stdin)
	out = os.Stdout
)

func send(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

func notify(method string, params any) {
	send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func reply(id json.RawMessage, result any) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func read() (msg, bool) {
	n := -1
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return msg{}, false
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, "content-length") {
			n, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if n < 0 {
		return msg{}, false
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(in, b); err != nil {
		return msg{}, false
	}
	var m msg
	_ = json.Unmarshal(b, &m)
	return m, true
}

func run(mode string, args []string) {
	switch mode {
	case "crash":
		os.Exit(3)
	case "sleep":
		time.Sleep(time.Hour)
	case "sleep-immune":
		// A grandchild that survives the group's SIGTERM: only a group kill
		// (or a job object) can remove it.
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("ready") // tells the parent the signal is ignored from now on
		time.Sleep(time.Hour)
	case "garbage":
		raw, _ := hex.DecodeString(args[0])
		out.Write(raw)
		if len(args) > 1 && args[1] == "exit" {
			return // closing stdout is what makes a truncated frame observable
		}
		time.Sleep(time.Hour)
	case "noinit":
		for {
			if _, ok := read(); !ok {
				time.Sleep(time.Hour)
			}
		}
	case "orphan-maker":
		// Leaves a grandchild behind and exits promptly when asked to stop.
		gc := immuneGrandchild()
		for {
			m, ok := read()
			if !ok {
				return
			}
			switch m.Method {
			case "plugin.initialize":
				notify("test.pids", map[string]int{"self": os.Getpid(), "grandchild": gc.Process.Pid})
				reply(m.ID, map[string]any{})
			case "plugin.shutdown":
				os.Exit(0)
			}
		}
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		gc := immuneGrandchild()
		for {
			m, ok := read()
			if !ok {
				time.Sleep(time.Hour) // ignore EOF too
			}
			if m.Method == "plugin.initialize" {
				notify("test.pids", map[string]int{"self": os.Getpid(), "grandchild": gc.Process.Pid})
				reply(m.ID, map[string]any{})
			}
		}
	default:
		serve(mode, args)
	}
}

// serve handles the protocol modes: report, crash-after-ready, stderr, flood.
func serve(mode string, args []string) {
	for {
		m, ok := read()
		if !ok {
			return
		}
		keep(m)
		switch m.Method {
		case "plugin.initialize":
			initParams = m.Params
			switch mode {
			case "report":
				cwd, _ := os.Getwd()
				notify("test.report", map[string]any{"env": os.Environ(), "args": args, "cwd": cwd, "init": m.Params})
			case "stderr":
				stderrStorm(args)
			case "stderr-init":
				fmt.Fprintln(os.Stderr, "init params:", string(m.Params)) // a plugin that logs its own settings
			}
			reply(m.ID, map[string]any{})
			if mode == "crash-after-ready" {
				time.Sleep(50 * time.Millisecond)
				os.Exit(3)
			}
		case "plugin.shutdown":
			os.Exit(0)
		case "echo":
			reply(m.ID, m.Params)
		case "callhost":
			// Ask the host something and hand back its answer. Notifications
			// that arrive meanwhile are kept for "drain".
			var p struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(m.Params, &p)
			var params any = map[string]any{}
			if len(p.Params) > 0 {
				params = p.Params
			}
			send(map[string]any{"jsonrpc": "2.0", "id": 9001, "method": p.Method, "params": params})
			for {
				r, ok := read()
				if !ok {
					return
				}
				if string(r.ID) == "9001" && r.Method == "" {
					reply(m.ID, map[string]any{"result": r.Result, "error": r.Error})
					break
				}
				keep(r)
			}
		case "drain":
			// The notifications received since the last drain, oldest first.
			reply(m.ID, received)
			received = nil
		case "getinit":
			reply(m.ID, initParams)
		case "stall":
			// Stop reading stdin for a while: the host's writes to this
			// plugin back up (a plugin that stopped reading).
			var p struct {
				Ms int `json:"ms"`
			}
			_ = json.Unmarshal(m.Params, &p)
			reply(m.ID, map[string]any{})
			time.Sleep(time.Duration(p.Ms) * time.Millisecond)
		case "notifyhost":
			notify("test.unknown", map[string]any{})
			reply(m.ID, map[string]any{})
		}
	}
}

// received holds the notifications the host pushed (events and the like).
var (
	received   []map[string]json.RawMessage
	initParams json.RawMessage
)

// keep records a notification (no id, has a method) other than the lifecycle
// ones.
func keep(m msg) {
	if len(m.ID) != 0 || m.Method == "" || strings.HasPrefix(m.Method, "plugin.") {
		return
	}
	method, _ := json.Marshal(m.Method)
	received = append(received, map[string]json.RawMessage{"method": method, "params": m.Params})
}

func stderrStorm(args []string) {
	e := os.Stderr
	fmt.Fprintln(e, "hello from the plugin")
	fmt.Fprintln(e, "api key sk-abcdefghijklmnopqrstuvwxyz0123456789 leaked")
	fmt.Fprintln(e, "TOKEN=hunter2hunter2hunter2")
	fmt.Fprintln(e, "escape \x1b[31mred\x1b[0m end")
	fmt.Fprintln(e, strings.Repeat("A", 100_000))
	fmt.Fprint(e, "no newline at the end")
	if len(args) > 0 {
		n, _ := strconv.Atoi(args[0])
		for i := 0; i < n; i++ {
			fmt.Fprintf(e, "\nflood %d", i)
		}
	}
	fmt.Fprintln(e)
}

// immuneGrandchild starts a grandchild that ignores SIGTERM and returns once
// it says so, so a test never races the signal against its start-up.
func immuneGrandchild() *exec.Cmd {
	gc := exec.Command(os.Args[0], Flag, "sleep-immune")
	r, err := gc.StdoutPipe()
	if err != nil || gc.Start() != nil {
		os.Exit(4)
	}
	if _, err := bufio.NewReader(r).ReadString('\n'); err != nil {
		os.Exit(5)
	}
	return gc
}
