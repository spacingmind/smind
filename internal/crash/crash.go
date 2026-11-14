// Package crash records panics as small JSON reports in a local crashes
// directory: reports are local only, the message and stack are redacted
// before anything is written, and nothing leaves the machine — no network,
// no process spawning, no upload.
package crash

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Config describes one recorder. The zero value of every optional field
// gets a default in New.
type Config struct {
	Dir       string // crashes directory, created lazily on the first report
	Version   string // build version recorded in each report
	Source    string // "daemon" for the daemon, "desktop-shell" for the shell
	Home      string // redacted as ~; defaults to os.UserHomeDir()
	SmindHome string // redacted as $SMIND_HOME

	Now func() time.Time // clock; defaults to time.Now

	MaxFiles     int           // default 20
	MaxFileBytes int           // default 64 << 10
	MaxDirBytes  int64         // default 2 << 20
	MaxPerMinute int           // default 10
	DedupeWindow time.Duration // default 5 * time.Minute
}

// Recorder writes bounded, redacted crash reports. A nil Recorder is a
// no-op that still recovers.
type Recorder struct {
	mu         sync.Mutex
	cfg        Config
	writeTimes []time.Time          // accepted writes, for the rolling rate limit
	lastSeen   map[string]time.Time // dedupe key -> time of the last accepted report
	suppressed int                  // reports dropped since the last one written
	tmpSeq     int
}

const msgMaxBytes = 300

var (
	defaultRecorder atomic.Pointer[Recorder]
	logErrOnce      sync.Once
	errLinkDir      = errors.New("crash directory is a link")
)

// New returns a recorder for cfg. It does no I/O: the directory is created
// by the first Record.
func New(cfg Config) *Recorder {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.Home = home
		}
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = 20
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 64 << 10
	}
	if cfg.MaxDirBytes <= 0 {
		cfg.MaxDirBytes = 2 << 20
	}
	if cfg.MaxPerMinute <= 0 {
		cfg.MaxPerMinute = 10
	}
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = 5 * time.Minute
	}
	return &Recorder{cfg: cfg, lastSeen: make(map[string]time.Time)}
}

// Init sets the process-default recorder.
func Init(cfg Config) { defaultRecorder.Store(New(cfg)) }

// Default returns the process-default recorder, or nil before Init.
func Default() *Recorder { return defaultRecorder.Load() }

// report is the on-disk JSON file, schema 1, shared with the desktop shell.
type report struct {
	Schema              int    `json:"schema"`
	Source              string `json:"source"`
	Version             string `json:"version"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	Time                string `json:"time"`
	Site                string `json:"site"`
	PanicType           string `json:"panicType"`
	Message             string `json:"message"`
	MessageWithheld     bool   `json:"messageWithheld"`
	Stack               string `json:"stack"`
	SuppressedSinceLast int    `json:"suppressedSinceLast"`
}

// Record writes one report and returns its path, or "" when nothing was
// written (dropped by the rate limit or the dedupe window, or an error). It
// never panics and never blocks beyond one small file write.
func (r *Recorder) Record(site string, v any, stack []byte) (path string) {
	// Everything below runs on a panicking goroutine or worse; a failure to
	// record must not become a second panic.
	defer func() { _ = recover() }()
	if r == nil {
		return ""
	}
	now := r.cfg.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.allow(now) || r.deduped(site, stack, now) {
		r.suppressed++
		return ""
	}
	rep := r.buildReport(site, v, stack, now)
	data, err := marshalBounded(rep, r.cfg.MaxFileBytes)
	if err != nil {
		logErr(err)
		return ""
	}
	path, err = r.store(data, now, sanitizeSite(site))
	if err != nil {
		logErr(err)
		return ""
	}
	r.writeTimes = append(r.writeTimes, now)
	r.lastSeen[site+"\x00"+dedupeFrame(stack)] = now
	r.pruneLastSeen(now)
	r.suppressed = 0
	return path
}

// Recover records a panic passed to a deferred call: `defer r.Recover("site")`.
// It swallows the panic; a nil receiver still recovers.
func (r *Recorder) Recover(site string) {
	v := recover()
	if v == nil || r == nil {
		return
	}
	r.Record(site, v, debug.Stack())
}

// Recover is Recover on the process-default recorder. It recovers even when
// no recorder was initialised. It must call recover() itself: a recover one
// call deeper (delegating to the method) would not stop the panic.
func Recover(site string) {
	v := recover()
	if v == nil {
		return
	}
	if r := Default(); r != nil {
		r.Record(site, v, debug.Stack())
	}
}

// RecordAndRepanic is for the daemon's main goroutine (`defer
// crash.RecordAndRepanic("main")`): a panic there must still end the process,
// but it is recorded first. It records on the process-default recorder and
// panics again with the same value.
func RecordAndRepanic(site string) {
	v := recover()
	if v == nil {
		return
	}
	if r := Default(); r != nil {
		r.Record(site, v, debug.Stack())
	}
	panic(v)
}

// Go runs fn on a new goroutine with Recover(site) installed, so a panic in
// fn ends that goroutine only.
func Go(site string, fn func()) {
	go func() {
		defer Recover(site)
		fn()
	}()
}

// allow enforces the rolling one-minute write budget.
func (r *Recorder) allow(now time.Time) bool {
	kept := r.writeTimes[:0]
	for _, t := range r.writeTimes {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	r.writeTimes = kept
	return len(r.writeTimes) < r.cfg.MaxPerMinute
}

// deduped reports whether the same panic (site + first frame outside
// runtime and this package) was already written inside the window.
func (r *Recorder) deduped(site string, stack []byte, now time.Time) bool {
	t, ok := r.lastSeen[site+"\x00"+dedupeFrame(stack)]
	return ok && now.Sub(t) < r.cfg.DedupeWindow
}

func (r *Recorder) pruneLastSeen(now time.Time) {
	for k, t := range r.lastSeen {
		if now.Sub(t) >= r.cfg.DedupeWindow {
			delete(r.lastSeen, k)
		}
	}
}

func (r *Recorder) buildReport(site string, v any, stack []byte, now time.Time) report {
	rep := report{
		Schema:              1,
		Source:              r.cfg.Source,
		Version:             r.cfg.Version,
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		Time:                now.UTC().Format(time.RFC3339Nano),
		Site:                site,
		PanicType:           fmt.Sprintf("%T", v),
		Stack:               Redact(string(stack), r.cfg.Home, r.cfg.SmindHome),
		SuppressedSinceLast: r.suppressed,
	}
	// Only a runtime.Error message is kept ("nil map", "index out of range"):
	// any other panic value may carry prompt text, file contents or secrets,
	// so it is recorded by type only.
	if re, ok := v.(runtime.Error); ok {
		rep.Message = truncate(Redact(re.Error(), r.cfg.Home, r.cfg.SmindHome), msgMaxBytes)
	} else {
		rep.MessageWithheld = true
	}
	return rep
}

const stackTruncated = "\n...[stack truncated]"

// marshalBounded marshals rep and, while it exceeds max bytes, cuts the
// stack (keeping its head) and appends the truncation marker until it fits.
func marshalBounded(rep report, max int) ([]byte, error) {
	data, err := json.Marshal(rep)
	if err != nil || len(data) <= max {
		return data, err
	}
	base, keep := rep.Stack, len(rep.Stack)
	for {
		keep -= len(data) - max + len(stackTruncated)
		if keep < 0 {
			keep = 0
		}
		rep.Stack = base[:keep] + stackTruncated
		data, err = json.Marshal(rep)
		if err != nil || len(data) <= max || keep == 0 {
			return data, err
		}
	}
}

// dedupeFrame returns the first stack frame outside runtime and this
// package: the frame that identifies where the panic happened. File and
// line rows are indented and skipped.
func dedupeFrame(stack []byte) string {
	for _, line := range strings.Split(string(stack), "\n") {
		if line == "" || line[0] == '\t' {
			continue
		}
		if strings.HasPrefix(line, "goroutine ") || strings.HasPrefix(line, "created by ") || strings.HasPrefix(line, "runtime/") {
			continue
		}
		if strings.Contains(line, "/internal/crash.") || strings.HasPrefix(line, "crash.") {
			continue
		}
		return line
	}
	return ""
}

// truncate cuts s to at most max bytes without splitting a rune.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	b := []byte(s[:max])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// logErr swallows every error after logging the first one per process.
func logErr(err error) {
	logErrOnce.Do(func() { log.Printf("crash: could not record a report: %v", err) })
}
