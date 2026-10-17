package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// serveCommands reads line commands from in and answers each with exactly
// one line on out (OK ... / ERR ...) until quit, EOF or ctx is done. Reading
// happens in its own goroutine so a stuck read never outlives the verdict.
func serveCommands(ctx context.Context, in io.Reader, out *lineWriter, d *daemon) {
	if in == nil {
		<-ctx.Done()
		return
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				return // EOF = quit
			}
			reply, quit := execute(ctx, d, line)
			if reply != "" {
				out.println(reply)
			}
			if quit {
				return
			}
		}
	}
}

func execute(ctx context.Context, d *daemon, line string) (reply string, quit bool) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", false
	}
	if f[0] == "quit" {
		return "OK bye", true
	}
	if f[0] == "restart-daemon" {
		d.restart(ctx)
		return "OK restarted", false
	}
	hub := d.current()
	if hub == nil {
		return "ERR daemon not running", false
	}
	resolve := func() (string, error) {
		if len(f) < 2 {
			return "", fmt.Errorf("usage: %s <id-or-prefix>", f[0])
		}
		return hub.ResolveDevice(f[1])
	}
	switch f[0] {
	case "window":
		maxNew, secs := 1, 600
		var err error
		if len(f) > 1 {
			if maxNew, err = strconv.Atoi(f[1]); err != nil {
				return "ERR bad maxNew", false
			}
		}
		if len(f) > 2 {
			if secs, err = strconv.Atoi(f[2]); err != nil {
				return "ERR bad seconds", false
			}
		}
		if err := hub.OpenPairingWindow(time.Duration(secs)*time.Second, maxNew); err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		return fmt.Sprintf("OK window %d %d", maxNew, secs), false
	case "approve":
		id, err := resolve()
		if err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		if _, err := hub.ApproveDevice(id); err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		return "OK approved " + id, false
	case "revoke":
		id, err := resolve()
		if err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		if _, err := hub.RevokeDevice(id); err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		return "OK revoked " + id, false
	case "rename":
		if len(f) < 3 {
			return "ERR usage: rename <id> <label>", false
		}
		id, err := hub.ResolveDevice(f[1])
		if err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		label := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), f[0]))
		label = strings.TrimSpace(strings.TrimPrefix(label, f[1]))
		if _, err := hub.RenameDevice(id, label); err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		return "OK renamed " + id, false
	case "list":
		list := hub.Devices()
		b, err := json.Marshal(list)
		if err != nil {
			return "ERR " + oneLine(err.Error()), false
		}
		if string(b) == "null" {
			b = []byte("[]")
		}
		return "DEVICES " + string(b), false
	}
	return "ERR unknown command " + f[0], false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
