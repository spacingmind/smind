package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
)

type report struct {
	Env  []string `json:"env"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
	Init struct {
		API          int            `json:"api"`
		SmindVersion string         `json:"smindVersion"`
		DataDir      string         `json:"dataDir"`
		Settings     map[string]any `json:"settings"`
	} `json:"init"`
}

func runReport(t *testing.T, sp Spec, o Options) (report, *Host) {
	t.Helper()
	rec := &recorder{}
	if o.Registry == nil {
		o.Registry = rec
	}
	if o.Policy == (Policy{}) {
		o.Policy = fastPolicy()
	}
	h := New(o)
	t.Cleanup(h.Close)
	h.Ensure(sp)
	waitState(t, h, sp.ID, StateRunning)
	var r report
	if rr, ok := o.Registry.(*recorder); ok {
		if err := json.Unmarshal(rr.wait(t, "test.report"), &r); err != nil {
			t.Fatal(err)
		}
	}
	return r, h
}

func TestProcess_ScrubbedEnv_NoSmindSecrets(t *testing.T) {
	sp := spec(t, "demo", "report")
	environ := []string{
		"SMIND_AUTH_TOKEN=smind-token-do-not-leak", "SMIND_HOME=/home/x/.smind", "ANTHROPIC_API_KEY=sk-ant-do-not-leak",
		"GITHUB_TOKEN=ghp_donotleak", "FOO=bar", "HOME=/Users/real", "PATH=/Users/real/bin:/usr/bin", "LANG=en_US.UTF-8", "TZ=UTC",
	}
	r, _ := runReport(t, sp, Options{Environ: func() []string { return environ }})
	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "TZ": true}
	if runtime.GOOS == "windows" {
		for _, k := range []string{"USERPROFILE", "TEMP", "TMP", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT"} {
			allowed[k] = true
		}
	}
	got := map[string]string{}
	for _, kv := range r.Env {
		k, v, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" {
			k = strings.ToUpper(k)
		}
		got[k] = v
		if !allowed[k] {
			t.Errorf("the plugin received %q: only allowlisted variables may pass", kv)
		}
		for _, secret := range []string{"do-not-leak", "donotleak", "/Users/real"} {
			if strings.Contains(v, secret) {
				t.Errorf("%s=%s carries a daemon secret/value", k, v)
			}
		}
	}
	if got["HOME"] != sp.DataDir {
		t.Errorf("HOME = %q, want the plugin data dir %q", got["HOME"], sp.DataDir)
	}
	if strings.Contains(got["PATH"], "/Users/real") {
		t.Errorf("PATH %q inherits the daemon's", got["PATH"])
	}
	if got["LANG"] != "en_US.UTF-8" {
		t.Errorf("LANG should pass through, got %q", got["LANG"])
	}
}

func TestProcess_WorkingDirIsPluginDir(t *testing.T) {
	sp := spec(t, "demo", "report")
	r, _ := runReport(t, sp, Options{})
	if !sameDir(t, r.Cwd, sp.Dir) {
		t.Errorf("cwd = %q, want %q", r.Cwd, sp.Dir)
	}
}

func TestProcess_InitializeCarriesSettings(t *testing.T) {
	sp := spec(t, "demo", "report")
	sp.Settings = map[string]any{"region": "eu", "n": 3}
	r, _ := runReport(t, sp, Options{})
	if r.Init.API != APIVersion || r.Init.SmindVersion != "9.9.9" || r.Init.DataDir != sp.DataDir {
		t.Fatalf("initialize params: %+v", r.Init)
	}
	if r.Init.Settings["region"] != "eu" || r.Init.Settings["n"] != float64(3) {
		t.Fatalf("settings: %+v", r.Init.Settings)
	}

	// With no settings the field is still present, as an empty object.
	sp2 := spec(t, "demo2", "report")
	rec := &recorder{}
	h := New(Options{Policy: fastPolicy(), Registry: rec})
	defer h.Close()
	h.Ensure(sp2)
	raw := rec.wait(t, "test.report")
	var generic struct {
		Init map[string]json.RawMessage `json:"init"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if string(generic.Init["settings"]) != "{}" {
		t.Fatalf("settings must be present and empty, got %q", generic.Init["settings"])
	}
}

func TestProcess_NoShellInterpolation(t *testing.T) {
	args := []string{";touch pwned", "$(id)", "`id`", "a b  c", "*", "'quoted'", `"dq"`, "x|y", "&& echo hi"}
	sp := spec(t, "demo", "report", args...)
	r, _ := runReport(t, sp, Options{})
	if len(r.Args) != len(args) {
		t.Fatalf("args = %q", r.Args)
	}
	for i := range args {
		if r.Args[i] != args[i] {
			t.Errorf("arg %d = %q, want it verbatim %q", i, r.Args[i], args[i])
		}
	}
}

func TestProcess_StdioFraming_Bidirectional(t *testing.T) {
	rec := &recorder{}
	h := New(Options{Policy: fastPolicy(), Registry: rec})
	defer h.Close()
	sp := spec(t, "demo", "report")
	h.Ensure(sp)
	waitState(t, h, "demo", StateRunning)

	// host -> plugin request and its answer
	res, err := h.Call(context.Background(), "demo", "echo", map[string]any{"a": []int{1, 2}, "s": "héllo"})
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		A []int  `json:"a"`
		S string `json:"s"`
	}
	if err := json.Unmarshal(res, &back); err != nil || len(back.A) != 2 || back.S != "héllo" {
		t.Fatalf("echo = %s %v", res, err)
	}
	// plugin -> host notification (test.report arrived) and plugin -> host
	// request answered with an error object
	rec.wait(t, "test.report")
	res, err = h.Call(context.Background(), "demo", "callhost", map[string]any{"method": "tasks.list"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res, &got); err != nil || got.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("the plugin should have seen method_not_found: %s %v", res, err)
	}
	// An unknown method on the plugin side is a JSON-RPC error, not a crash.
	if _, err := h.Call(context.Background(), "demo", "nope", nil); err == nil {
		// the helper ignores unknown requests; the call must time out instead
		t.Fatal("expected a timeout for a request the plugin never answers")
	}
}

func TestHost_PluginToHost_UnknownMethodRefused(t *testing.T) {
	rec := &recorder{}
	h := New(Options{Policy: fastPolicy(), Registry: rec})
	defer h.Close()
	h.Ensure(spec(t, "demo", "report"))
	waitState(t, h, "demo", StateRunning)
	for _, m := range []string{"tasks.list", "events.subscribe", "plugin.initialize", "../../etc/passwd"} {
		res, err := h.Call(context.Background(), "demo", "callhost", map[string]any{"method": m})
		if err != nil {
			t.Fatalf("%q: %v", m, err)
		}
		if !strings.Contains(string(res), `"code":-32601`) {
			t.Errorf("%q should be refused with method_not_found, got %s", m, res)
		}
	}
	// A notification the host does not know must not disturb the connection.
	if _, err := h.Call(context.Background(), "demo", "notifyhost", nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.Status("demo"); st.State != StateRunning || st.Restarts != 0 {
		t.Fatalf("refusals must not restart the plugin: %+v", st)
	}
}

func frames(s string) *bufio.Reader { return bufio.NewReaderSize(strings.NewReader(s), 1024) }

func TestProcess_FramingCaps_OversizeAndMalformed(t *testing.T) {
	lim := DefaultPolicy().Limits
	okBody := `{"jsonrpc":"2.0","method":"x"}`
	cases := []struct {
		name string
		in   string
		code string // "" = a valid frame, "eof" = clean EOF
	}{
		{"valid", "Content-Length: 30\r\n\r\n" + okBody, ""},
		{"valid with content-type", "Content-Length: 30\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n" + okBody, ""},
		{"clean eof", "", "eof"},
		{"oversize length", "Content-Length: 1073741824\r\n\r\n", FrameBodyTooLarge},
		{"just over the cap", "Content-Length: 1048577\r\n\r\n", FrameBodyTooLarge},
		{"huge digits", "Content-Length: 99999999999999999999\r\n\r\n", FrameBadLength},
		{"negative", "Content-Length: -5\r\n\r\n", FrameBadLength},
		{"plus sign", "Content-Length: +5\r\n\r\nhello", FrameBadLength},
		{"non-numeric", "Content-Length: abc\r\n\r\n", FrameBadLength},
		{"hex", "Content-Length: 0x10\r\n\r\n", FrameBadLength},
		{"empty value", "Content-Length:\r\n\r\n", FrameBadLength},
		{"zero", "Content-Length: 0\r\n\r\n", FrameBadLength},
		{"truncated body", "Content-Length: 100\r\n\r\n{\"jsonrpc\"", FrameTruncated},
		{"truncated header", "Content-Length: 10\r\n", FrameTruncated},
		{"duplicate length", "Content-Length: 30\r\nContent-Length: 30\r\n\r\n" + okBody, FrameDuplicate},
		{"duplicate content-type", "Content-Length: 30\r\nContent-Type: a\r\nContent-Type: a\r\n\r\n" + okBody, FrameDuplicate},
		{"missing length", "Content-Type: x\r\n\r\n" + okBody, FrameMissingLength},
		{"bare LF header", "Content-Length: 30\n\n" + okBody, FrameBadHeader},
		{"unknown header", "X-Evil: 1\r\nContent-Length: 30\r\n\r\n" + okBody, FrameBadHeader},
		{"no colon", "Content-Length 30\r\n\r\n" + okBody, FrameBadHeader},
		{"binary garbage", "\x00\x01\x02\xff\xfe garbage \x80\r\n\r\n", FrameBadHeader},
		{"control byte in header", "Content-Length: 3\x07\r\n\r\nabc", FrameBadHeader},
		{"header line too long", "Content-Length: 30\r\nX: " + strings.Repeat("a", 4000) + "\r\n\r\n", FrameHeaderTooLarge},
		{"no newline ever", strings.Repeat("A", 100000), FrameHeaderTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			body, err := readFrame(frames(c.in), lim)
			runtime.ReadMemStats(&after)
			if after.TotalAlloc-before.TotalAlloc > 4<<20 {
				t.Errorf("allocated %d bytes reading %q: an oversize length must not be allocated", after.TotalAlloc-before.TotalAlloc, c.name)
			}
			switch c.code {
			case "":
				if err != nil || string(body) != okBody {
					t.Fatalf("valid frame: %q %v", body, err)
				}
			case "eof":
				if !errors.Is(err, io.EOF) {
					t.Fatalf("want EOF, got %v", err)
				}
			default:
				var fe *FrameError
				if !errors.As(err, &fe) || fe.Code != c.code {
					t.Fatalf("want a %s frame error, got %v", c.code, err)
				}
			}
		})
	}
}

// A frame that is valid framing but not a JSON-RPC message ends the connection too.
func TestProcess_FramingCaps_BadJSONRejected(t *testing.T) {
	for name, body := range map[string]string{
		"not json": "hello world", "array batch": `[{"jsonrpc":"2.0","method":"x"}]`, "wrong version": `{"jsonrpc":"1.0","method":"x"}`,
		"empty object": `{"jsonrpc":"2.0"}`, "object id": `{"jsonrpc":"2.0","id":{},"method":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body
			assertProcessClosesOn(t, raw, "")
		})
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestProcess_FramingCaps_ClosesConnectionAndKillsProcess(t *testing.T) {
	for name, raw := range map[string]string{
		"oversize":  "Content-Length: 2000000000\r\n\r\n",
		"garbage":   "\x00\xff\xfe\x01garbage\r\n\r\n",
		"truncated": "Content-Length: 500\r\n\r\n{",
		"negative":  "Content-Length: -1\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertProcessClosesOn(t, raw, map[string]string{
				"oversize": FrameBodyTooLarge, "garbage": FrameBadHeader, "negative": FrameBadLength, "truncated": FrameTruncated,
			}[name])
		})
	}
}

// assertProcessClosesOn runs the helper in garbage mode (it writes raw bytes
// before the handshake) and checks the host closed the connection with a
// typed error and killed the process, without waiting for the start timeout.
func assertProcessClosesOn(t *testing.T, raw, code string) {
	assertClose(t, raw, code, code == FrameTruncated)
}

func assertClose(t *testing.T, raw, code string, exits bool) {
	t.Helper()
	args := []string{hex.EncodeToString([]byte(raw))}
	if exits {
		args = append(args, "exit")
	}
	sp := spec(t, "demo", "garbage", args...)
	pol := fastPolicy()
	pol.StartTimeout = 20 * time.Second // the close, not the timeout, must end it
	pol.BackoffBase, pol.BackoffCap = time.Hour, time.Hour
	h := New(Options{Policy: pol})
	defer h.Close()
	h.Ensure(sp)
	st := waitState(t, h, "demo", StateRestarting)
	if !strings.Contains(st.LastError, "plugin protocol") {
		t.Fatalf("want a typed protocol error, got %q", st.LastError)
	}
	if code != "" && !strings.Contains(st.LastError, code) {
		t.Fatalf("want code %s in %q", code, st.LastError)
	}
	lines := h.Logs("demo", 0)
	var sawProto bool
	for _, l := range lines {
		if strings.Contains(l.Text, "plugin protocol") {
			sawProto = true
		}
	}
	if !sawProto {
		t.Fatalf("the protocol error must be in the plugin log: %+v", lines)
	}
	// A process that was still alive when its connection broke is killed.
	for _, l := range lines {
		if i := strings.Index(l.Text, "(pid "); i >= 0 && !exits {
			pid, _ := strconv.Atoi(strings.TrimSuffix(l.Text[i+5:], ")"))
			waitFor(t, 5*time.Second, "the misbehaving process to be killed", func() bool { return !alive(pid) })
		}
	}
}

func TestConn_InboundRequestCap(t *testing.T) {
	// MaxInbound handlers block; the next request is answered busy, not queued.
	block := make(chan struct{})
	reg := regFuncs{req: func(ctx context.Context, _, _ string, _ json.RawMessage) (any, *RPCError) {
		<-block
		return "ok", nil
	}}
	pr, pw := io.Pipe()
	out := &safeWriter{w: &bytes.Buffer{}}
	lim := DefaultPolicy().Limits
	lim.MaxInbound = 2
	c := newConn("p", pr, out, lim, reg, newLog(DefaultPolicy().Log, nil, time.Now), time.Second, nil)
	go c.readLoop()
	for i := 1; i <= 4; i++ {
		body := `{"jsonrpc":"2.0","id":` + itoa(i) + `,"method":"m"}`
		writeFrame(pw, []byte(body))
	}
	waitFor(t, 5*time.Second, "busy answers", func() bool { return strings.Count(out.String(), "-32002") == 2 })
	close(block)
	pw.Close()
}

func TestConn_OutstandingCapAndTimeout(t *testing.T) {
	pr, _ := io.Pipe() // the peer never answers
	out := &safeWriter{w: &bytes.Buffer{}}
	lim := DefaultPolicy().Limits
	lim.MaxOutstanding = 2
	c := newConn("p", pr, out, lim, DenyAll, newLog(DefaultPolicy().Log, nil, time.Now), time.Second, nil)
	go c.readLoop()
	done := make(chan error, 3)
	for i := 0; i < 2; i++ {
		go func() { _, err := c.call(context.Background(), "x", nil, 300*time.Millisecond); done <- err }()
	}
	waitFor(t, 2*time.Second, "two outstanding", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.pending) == 2 })
	if _, err := c.call(context.Background(), "x", nil, time.Second); !errors.Is(err, ErrBusy) {
		t.Fatalf("third call: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, ErrTimeout) {
			t.Fatalf("want the per-request timeout, got %v", err)
		}
	}
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("timed-out calls left %d pending entries", n)
	}
}

func TestProcess_StderrLog_BoundedAndRedacted(t *testing.T) {
	sp := spec(t, "demo", "stderr", "5000")
	pol := fastPolicy()
	pol.Log = LogLimits{MaxLines: 50, MaxBytes: 8 << 10, MaxLineBytes: 300, ReadLine: 4096, LinesPerSec: 100}
	red := redact.New([]string{"hunter2hunter2hunter2"}, nil)
	h := New(Options{Policy: pol, Redact: red.String})
	defer h.Close()
	h.Ensure(sp)
	waitState(t, h, "demo", StateRunning)
	var lines []LogEntry
	waitFor(t, 10*time.Second, "the flood to be read", func() bool {
		lines = h.Logs("demo", 0)
		for _, l := range lines {
			if strings.Contains(l.Text, "stderr lines dropped") || strings.Contains(l.Text, "flood 4999") {
				return true
			}
		}
		return false
	})
	l := h.logFor("demo")
	n, b, dropped := l.Stats()
	if n > pol.Log.MaxLines || b > pol.Log.MaxBytes || dropped == 0 {
		t.Fatalf("the log is not bounded: %d lines, %d bytes, %d dropped (caps %d/%d)", n, b, dropped, pol.Log.MaxLines, pol.Log.MaxBytes)
	}
	for _, e := range h.Logs("demo", 0) {
		for _, bad := range []string{"sk-abcdefghijklmnopqrstuvwxyz0123456789", "hunter2hunter2hunter2", "\x1b"} {
			if strings.Contains(e.Text, bad) {
				t.Errorf("log line holds %q: %q", bad, e.Text)
			}
		}
		if len(e.Text) > pol.Log.MaxLineBytes+len("...[truncated]") {
			t.Errorf("line of %d bytes exceeds the cap", len(e.Text))
		}
	}
}

func TestProcess_StderrLog_EarlyLinesRedactedAndNeutralized(t *testing.T) {
	sp := spec(t, "demo", "stderr")
	red := redact.New([]string{"hunter2hunter2hunter2"}, nil)
	h := New(Options{Policy: fastPolicy(), Redact: red.String})
	defer h.Close()
	h.Ensure(sp)
	waitState(t, h, "demo", StateRunning)
	var text string
	waitFor(t, 10*time.Second, "the first lines", func() bool {
		var sb strings.Builder
		for _, e := range h.Logs("demo", 0) {
			sb.WriteString(e.Text + "\n")
		}
		text = sb.String()
		return strings.Contains(text, "no newline at the end")
	})
	if !strings.Contains(text, "hello from the plugin") {
		t.Fatalf("log lost a plain line:\n%s", text)
	}
	if strings.Contains(text, "sk-abcdefghij") || strings.Contains(text, "hunter2") || strings.ContainsRune(text, 0x1b) {
		t.Fatalf("secret or escape in the log:\n%s", text)
	}
	if !strings.Contains(text, "[smind] started") || !strings.Contains(text, "[smind] ready") {
		t.Fatalf("host messages missing:\n%s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("nothing was redacted:\n%s", text)
	}
}

type regFuncs struct {
	req func(ctx context.Context, plugin, method string, params json.RawMessage) (any, *RPCError)
}

func (r regFuncs) Request(ctx context.Context, p, m string, params json.RawMessage) (any, *RPCError) {
	return r.req(ctx, p, m, params)
}
func (regFuncs) Notify(string, string, json.RawMessage) {}

var _ = hosttest.Flag
