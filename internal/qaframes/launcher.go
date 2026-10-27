package qaframes

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed driver.cjs
var driverJS []byte

// RealLauncher starts the real daemon and the real node driver.
type RealLauncher struct {
	Bin       string   // use this smind binary instead of building one
	NoBuild   bool     // skip the web build (use the dist already present)
	FakeAgent bool     // build the fake ACP agent and point the daemon's GLM provider at it
	SeedFile  string   // JSON [{method, params}] RPCs run against the isolated daemon
	PassEnv   []string // names of caller env vars to pass to the daemon (live sends only)
	AllowSend bool     // inherit PATH so a provider CLI can be found; otherwise PATH is minimal
	RepoRoot  string
	Log       io.Writer
	StartWait time.Duration
}

func (l *RealLauncher) logf(f string, a ...any) {
	if l.Log != nil {
		fmt.Fprintf(l.Log, "qaframes: "+f+"\n", a...)
	}
}

func (l *RealLauncher) runTool(ctx context.Context, sup *Supervisor, dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := sup.Run(ctx, cmd); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, tail(out.String()))
	}
	return nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 800 {
		s = "..." + s[len(s)-800:]
	}
	return s
}

// Prepare builds the web bundle and the binary in a checkout, or uses --bin.
func (l *RealLauncher) Prepare(ctx context.Context, sup *Supervisor, env Env, tmp string) (string, error) {
	bin := l.Bin
	if bin == "" {
		if l.RepoRoot == "" {
			return "", fmt.Errorf("no --bin and not in a checkout: cannot build the web UI")
		}
		if !l.NoBuild {
			if _, err := env.LookPath("bun"); err != nil {
				return "", fmt.Errorf("bun is not on PATH: cannot build the web UI (use --bin with a binary that embeds it, or --no-build if internal/server/dist is already built)")
			}
			l.logf("building web UI")
			if err := l.runTool(ctx, sup, filepath.Join(l.RepoRoot, "web"), "bun", "install", "--frozen-lockfile"); err != nil {
				return "", err
			}
			if err := l.runTool(ctx, sup, filepath.Join(l.RepoRoot, "web"), "bun", "run", "--filter", "@smind/ui", "build"); err != nil {
				return "", err
			}
		}
		bin = filepath.Join(tmp, "smind")
		l.logf("building smind")
		if err := l.runTool(ctx, sup, l.RepoRoot, "go", "build", "-o", bin, "./cmd/smind"); err != nil {
			return "", err
		}
	}
	if l.FakeAgent {
		if l.RepoRoot == "" {
			return "", fmt.Errorf("--fake-agent needs a checkout")
		}
		if err := l.runTool(ctx, sup, l.RepoRoot, "go", "build", "-o", filepath.Join(tmp, "fakeagent"), "./internal/taskrunner/fakeagent"); err != nil {
			return "", err
		}
	}
	return bin, nil
}

// StartDaemon runs `smind serve` with a scrubbed environment: a fresh home,
// HOME, a loopback port and a minimal PATH, so no provider CLI is found and no
// credential is inherited (a send cannot reach a live model unless allowed).
func (l *RealLauncher) StartDaemon(ctx context.Context, sup *Supervisor, bin, home string, port int) (string, error) {
	listen := fmt.Sprintf("127.0.0.1:%d", port)
	env := l.daemonEnv(home, listen)
	cmd := exec.Command(bin, "serve")
	cmd.Env = env
	logf, err := os.Create(filepath.Join(filepath.Dir(home), "daemon.log"))
	if err != nil {
		return "", err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := sup.Start(cmd); err != nil {
		return "", err
	}
	base := "http://" + listen
	wait := l.StartWait
	if wait == 0 {
		wait = 45 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !Alive(cmd.Process.Pid) {
			return "", fmt.Errorf("the isolated daemon exited at start: %s", tail(readFile(logf.Name())))
		}
		resp, err := http.Get(base + "/")
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			if resp.StatusCode == 200 {
				if strings.Contains(string(b), "Web UI not built yet") {
					return "", fmt.Errorf("the daemon serves the placeholder: the web UI is not built into this binary")
				}
				break
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the isolated daemon did not answer within %s", wait)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if l.SeedFile != "" {
		if err := SeedDaemon(ctx, base, home, l.SeedFile); err != nil {
			return "", fmt.Errorf("seed: %w", err)
		}
	}
	return base, nil
}

// daemonEnv is the daemon's whole environment: nothing is inherited except
// what is named here, so the Human's HOME, SMIND_HOME, credentials and PATH
// (hence provider CLIs) never reach it unless a live send was allowed.
func (l *RealLauncher) daemonEnv(home, listen string) []string {
	path := "/usr/bin:/bin"
	if l.AllowSend {
		path = os.Getenv("PATH")
	}
	env := []string{"HOME=" + home, "SMIND_HOME=" + home, "SMIND_LISTEN=" + listen, "PATH=" + path, "TMPDIR=" + os.TempDir()}
	if l.FakeAgent {
		env = append(env, "SMIND_ACP_COMMAND="+filepath.Join(filepath.Dir(home), "fakeagent"))
	}
	for _, n := range l.PassEnv {
		if v, ok := os.LookupEnv(n); ok {
			env = append(env, n+"="+v)
		}
	}
	return env
}

func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }

// RunDriver writes the embedded driver to a temp file, runs it under the
// supervisor and reads one JSON event per line.
func (l *RealLauncher) RunDriver(ctx context.Context, sup *Supervisor, b Browser, base string, shots []DriverShot, allowSend bool, model string) ([]DriverResult, error) {
	dir, err := os.MkdirTemp("", "smind-qa-driver-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	js := filepath.Join(dir, "driver.cjs")
	if err := os.WriteFile(js, driverJS, 0o600); err != nil {
		return nil, err
	}
	job, _ := json.Marshal(map[string]any{
		"base": base, "chrome": b.Chrome, "playwrightDir": b.PlaywrightDir, "playwrightPkg": b.PlaywrightPkg,
		"shots": shots, "allowSend": allowSend, "model": model,
	})
	cmd := exec.Command(b.Node, js)
	cmd.Stdin = bytes.NewReader(job)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	cmd.Dir = dir
	if err := sup.Start(cmd); err != nil {
		return nil, err
	}
	var results []DriverResult
	var fatal string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	lines := make(chan string)
	go func() {
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	total := time.After(time.Duration(len(shots)+1) * 90 * time.Second)
loop:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-total:
			return nil, fmt.Errorf("the browser driver timed out")
		case ln, ok := <-lines:
			if !ok {
				break loop
			}
			var ev struct {
				Event string `json:"event"`
				PID   int    `json:"pid"`
				Error string `json:"error"`
				DriverResult
			}
			if json.Unmarshal([]byte(ln), &ev) != nil {
				continue
			}
			switch ev.Event {
			case "browser":
				sup.Adopt(ev.PID)
			case "shot":
				results = append(results, ev.DriverResult)
			case "fatal":
				fatal = ev.Error
			}
		}
	}
	if fatal != "" {
		return nil, fmt.Errorf("browser driver: %s", tail(fatal))
	}
	if len(results) < len(shots) {
		return nil, fmt.Errorf("browser driver returned %d of %d shots: %s", len(results), len(shots), tail(errBuf.String()))
	}
	return results, nil
}
