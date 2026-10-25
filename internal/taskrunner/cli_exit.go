package taskrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"

	"github.com/spacingmind/smind/internal/learning/redact"
)

// FailureKindCLIExited is the typed failure of a claude-native run whose CLI
// process ended the message stream before sending a result message (crash,
// external kill, out of memory). The run has no result and so no final usage;
// its session was stored at init, so a follow-up prompt resumes it.
const FailureKindCLIExited = "cli_exited"

// UsageSourcePartialStream marks usage summed from the CLI's streamed API
// events (message_start/message_delta) of a turn that ended without its
// result message: only what the stream reported, so a lower bound, and no
// cost (the stream carries none; ADR-0020 never estimates one).
const UsageSourcePartialStream UsageSource = "partial_stream"

const (
	// cliStderrKeep is how much raw stderr is buffered (the tail wins).
	cliStderrKeep = 8 << 10
	// cliStderrShown is the redacted stderr tail put in the error text.
	cliStderrShown = 2 << 10
	// cliStderrDrainWait bounds how long a CLI exit waits for the stderr pipe
	// to reach EOF (a grandchild holding it open would otherwise never end it).
	cliStderrDrainWait = 2 * time.Second
	// cliStderrIncomplete is appended to the stderr text when the drain timed out.
	cliStderrIncomplete = " (stderr incomplete)"
)

// CLIExitError is a claude-native CLI that ended its stream without a result.
type CLIExitError struct {
	// ExitCode is the CLI's exit code; -1 when it was killed by a signal or
	// the code was not observed: the SDK reads it while racing the process
	// reaper and reports no signal, so even a plain exit can read -1.
	ExitCode int
	// Stderr is the redacted tail of the CLI's stderr ("" = it wrote none).
	Stderr string
	// StderrIncomplete: the stderr pipe was still open when the drain bound
	// ran out, so Stderr may miss the CLI's last output.
	StderrIncomplete bool
	// LastEvent is the type of the last message the CLI streamed ("" = none)
	// and Since how long before the exit it arrived.
	LastEvent string
	Since     time.Duration
	// Duration is how long the turn ran before the exit.
	Duration time.Duration
	Err      error
}

func (e *CLIExitError) Error() string {
	var b strings.Builder
	b.WriteString("claude CLI exited before sending a result message (")
	if e.ExitCode == -1 {
		b.WriteString("exit status unknown: killed by a signal such as SIGKILL, possibly out of memory, or the exit was not yet observed")
	} else {
		fmt.Fprintf(&b, "exit code %d", e.ExitCode)
	}
	fmt.Fprintf(&b, ") after %s", e.Duration.Round(time.Second))
	if e.LastEvent != "" {
		fmt.Fprintf(&b, "; last event: %s, %s before the exit", e.LastEvent, e.Since.Round(time.Second))
	} else {
		b.WriteString("; no event received")
	}
	if e.Stderr != "" {
		fmt.Fprintf(&b, "; stderr: %s", e.Stderr)
	} else {
		b.WriteString("; stderr: empty")
	}
	if e.StderrIncomplete {
		b.WriteString(cliStderrIncomplete)
	}
	b.WriteString(". Send a follow-up prompt to resume the session")
	return b.String()
}

func (e *CLIExitError) Unwrap() error { return e.Err }

// tailBuffer keeps the last cliStderrKeep bytes written to it; it is the
// io.Writer handed to the SDK as the CLI's stderr.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > cliStderrKeep {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-cliStderrKeep:]...)
	}
	return len(p), nil
}

// stderrCapture is the CLI's stderr as the SDK's log writer. The SDK hands a
// non-*os.File writer to exec, which copies the child's stderr pipe into it
// with io.Copy; that calls ReadFrom, which returns exactly when the pipe
// reaches EOF (the CLI and anything it spawned closed stderr). That is the
// signal the SDK does not expose: the message stream ends at stdout EOF, which
// can precede the last stderr bytes. A pipe *os.File would give the same signal
// but leaves exec no copy goroutine to hold cmd.Wait back, so Wait closes the
// stdout pipe at process exit and drops stdout the SDK has not yet read.
type stderrCapture struct {
	tail tailBuffer

	mu      sync.Mutex
	started int           // ReadFrom calls begun (one per spawned CLI)
	active  int           // ReadFrom calls not yet at EOF
	changed chan struct{} // closed and replaced whenever started/active change
}

func newStderrCapture() *stderrCapture { return &stderrCapture{changed: make(chan struct{})} }

func (c *stderrCapture) Write(p []byte) (int, error) { return c.tail.Write(p) }

// ReadFrom copies r (the CLI's stderr pipe) into the tail until EOF.
func (c *stderrCapture) ReadFrom(r io.Reader) (int64, error) {
	c.bump(1, 1)
	defer c.bump(0, -1)
	return io.Copy(writerOnly{&c.tail}, r)
}

// writerOnly hides tailBuffer's other methods so io.Copy does not loop back here.
type writerOnly struct{ io.Writer }

func (c *stderrCapture) bump(started, active int) {
	c.mu.Lock()
	c.started += started
	c.active += active
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

// drain waits until every spawned CLI's stderr reached EOF, at most max; false
// means it timed out (a grandchild may hold the pipe open, or the copy had not
// begun) and the tail may be incomplete.
func (c *stderrCapture) drain(max time.Duration) bool {
	t := time.NewTimer(max)
	defer t.Stop()
	for {
		c.mu.Lock()
		done, ch := c.started > 0 && c.active == 0, c.changed
		c.mu.Unlock()
		if done {
			return true
		}
		select {
		case <-ch:
		case <-t.C:
			return false
		}
	}
}

// redacted is the stderr tail with secrets removed (exactSecrets by value,
// then the shared rule set and the home directory), capped at cliStderrShown
// and flattened to one line.
func (t *tailBuffer) redacted(exactSecrets []string) string {
	t.mu.Lock()
	s := string(t.buf)
	t.mu.Unlock()
	var homes []string
	if h, err := os.UserHomeDir(); err == nil {
		homes = append(homes, h)
	}
	s = redact.New(exactSecrets, homes).String(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " | ")), " ")
	if len(s) > cliStderrShown {
		s = "…" + s[len(s)-cliStderrShown:]
	}
	return s
}

// isCLIExit reports whether err is the SDK's "stream ended with no result"
// failure (its message, or the ProcessError the client keeps for it).
func isCLIExit(err error) bool {
	var pe *claudecode.ProcessError
	return errors.As(err, &pe) || (err != nil && strings.Contains(err.Error(), "cli exited before sending a result message"))
}

// claudeStreamProbe watches one turn's SDK messages for what a CLI exit
// leaves behind: the last message seen and the API usage the stream reported.
// The SDK's AssistantMessage drops message.usage, so the usage is summed from
// the raw stream events (always on: WithIncludePartialMessages).
type claudeStreamProbe struct {
	mu       sync.Mutex
	started  time.Time
	lastType string
	lastAt   time.Time
	// per API message: input side from message_start, output from the latest
	// message_start/message_delta; summed over the turn.
	in, cacheRead, cacheWrite, out int64
	cur                            struct{ in, cacheRead, cacheWrite, out int64 }
	model                          string
	reported                       bool
}

func newClaudeStreamProbe() *claudeStreamProbe { return &claudeStreamProbe{started: time.Now()} }

type streamUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

func (p *claudeStreamProbe) observe(msg claudecode.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastType, p.lastAt = claudeMessageType(msg), time.Now()
	se, ok := msg.(claudecode.StreamEvent)
	if !ok {
		return
	}
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Model string      `json:"model"`
			Usage streamUsage `json:"usage"`
		} `json:"message"`
		Usage streamUsage `json:"usage"`
	}
	if json.Unmarshal(se.Event, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		p.settle()
		if ev.Message.Model != "" {
			p.model = ev.Message.Model
		}
		p.apply(ev.Message.Usage)
	case "message_delta":
		p.apply(ev.Usage)
	}
}

// apply folds a usage object into the message in progress: a field present
// replaces what the message had (the API reports running values).
func (p *claudeStreamProbe) apply(u streamUsage) {
	set := func(dst *int64, v *int64) {
		if v != nil {
			*dst = *v
			p.reported = true
		}
	}
	set(&p.cur.in, u.InputTokens)
	set(&p.cur.out, u.OutputTokens)
	set(&p.cur.cacheRead, u.CacheReadInputTokens)
	set(&p.cur.cacheWrite, u.CacheCreationInputTokens)
}

// settle adds the finished message in progress to the turn's totals.
func (p *claudeStreamProbe) settle() {
	p.in += p.cur.in
	p.out += p.cur.out
	p.cacheRead += p.cur.cacheRead
	p.cacheWrite += p.cur.cacheWrite
	p.cur = struct{ in, cacheRead, cacheWrite, out int64 }{}
}

// partialUsage is what the stream reported so far; nil when it reported
// nothing. No cost: the stream carries none.
func (p *claudeStreamProbe) partialUsage() *Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.reported {
		return nil
	}
	p.settle()
	u := &Usage{
		Input: ptr(p.in), CachedInput: ptr(p.cacheRead), CacheWrite: ptr(p.cacheWrite), Output: ptr(p.out),
		Source: UsageSourcePartialStream,
	}
	if p.model != "" {
		u.Model = ptr(p.model)
	}
	return u
}

// exitError builds the typed error for a CLI that ended the turn early.
func (p *claudeStreamProbe) exitError(cause error, exitCode int, stderr string) *CLIExitError {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := &CLIExitError{ExitCode: exitCode, Stderr: stderr, LastEvent: p.lastType, Duration: time.Since(p.started), Err: cause}
	if p.lastType != "" {
		e.Since = time.Since(p.lastAt)
	}
	return e
}

// claudeMessageType names an SDK message for the "last event" text.
func claudeMessageType(msg claudecode.Message) string {
	switch msg.(type) {
	case claudecode.AssistantMessage:
		return "assistant"
	case claudecode.UserMessage:
		return "user"
	case claudecode.StreamEvent:
		return "stream_event"
	case claudecode.SystemMessage:
		return "system"
	case claudecode.RateLimitEvent:
		return "rate_limit_event"
	default:
		return fmt.Sprintf("%T", msg)
	}
}
