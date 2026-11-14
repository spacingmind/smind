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
	"path/filepath"
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
	mu         sync.Mutex // guards the state below, never held across disk I/O
	ioMu       sync.Mutex // one writer at a time; a second concurrent report is dropped, not waited for
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
// written (dropped by the rate limit, the dedupe window or because another
// report is being written right now; or an error). It never panics. The state
// lock is not held across disk I/O, and a second report arriving while one is
// being written is dropped and counted rather than queued behind a slow disk;
// the one goroutine that is writing still waits for its own small file write.
//
// A failed write counts like a successful one for the rate limit and the
// dedupe window: a panic loop on a full or unwritable disk retries the whole
// path at most MaxPerMinute times a minute, not on every panic.
func (r *Recorder) Record(site string, v any, stack []byte) (path string) {
	// Everything below runs on a panicking goroutine or worse; a failure to
	// record must not become a second panic.
	defer func() { _ = recover() }()
	if r == nil {
		return ""
	}
	now := r.cfg.Now()
	key := site + "\x00" + dedupeFrame(stack)

	r.mu.Lock()
	if !r.allow(now) || r.deduped(key, now) {
		r.suppressed++
		r.mu.Unlock()
		return ""
	}
	// Reserve the slot before the write: failures count too.
	r.writeTimes = append(r.writeTimes, now)
	r.lastSeen[key] = now
	r.pruneLastSeen(now)
	rep := r.buildReport(site, v, stack, now)
	rep.SuppressedSinceLast = r.suppressed
	r.mu.Unlock()

	if !r.ioMu.TryLock() {
		r.mu.Lock()
		r.suppressed++
		r.mu.Unlock()
		return ""
	}
	defer r.ioMu.Unlock()
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
	r.mu.Lock()
	r.suppressed = 0
	r.mu.Unlock()
	return path
}

// Recover records a panic passed to a deferred call: `defer r.Recover("site")`.
// It swallows the panic; a nil receiver still recovers.
func (r *Recorder) Recover(site string) {
	v := recover()
	if v == nil || r == nil {
		return
	}
	logRecovered(site, r.Record(site, v, debug.Stack()))
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
		logRecovered(site, r.Record(site, v, debug.Stack()))
	}
}

// logRecovered says in the daemon log that a panic was contained, so a
// swallowed panic is never silent. Only the site (a code constant) and the
// report's file name are logged, never the panic value.
func logRecovered(site, path string) {
	if path == "" {
		log.Printf("crash: recovered a panic in %s (no report written: rate limit, duplicate or write failure)", site)
		return
	}
	log.Printf("crash: recovered a panic in %s; report %s", site, filepath.Base(path))
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

// Go runs fn on a new goroutine and CONTAINS a panic in it: the panic is
// recorded and logged and only that goroutine ends. Use it only where losing
// the goroutine is harmless or something else makes the loss visible (see
// GoThen); a long-lived loop or server whose silent death would leave the
// daemon half-alive must use GoFatal. Every call site is listed with its
// policy and reason in coverage_test.go.
func Go(site string, fn func()) {
	go func() {
		defer Recover(site)
		fn()
	}()
}

// GoThen is Go plus a compensating action: after the panic is recorded,
// onPanic runs (its own panic is swallowed) so the failure shows up where a
// user or a peer goroutine would otherwise wait forever: close a connection,
// cancel and finish a run.
func GoThen(site string, fn func(), onPanic func()) {
	go func() {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if r := Default(); r != nil {
				logRecovered(site, r.Record(site, v, debug.Stack()))
			}
			func() {
				defer func() { _ = recover() }()
				if onPanic != nil {
					onPanic()
				}
			}()
		}()
		fn()
	}()
}

// GoFatal runs fn on a new goroutine and records a panic in it, then lets the
// process die as it always did: for the goroutines that are the daemon (the
// listener, the relay bridge, background loops, per-session pumps) a contained
// panic would leave a daemon that is up but silently without that part.
func GoFatal(site string, fn func()) {
	go func() {
		defer RecordAndRepanic(site)
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
func (r *Recorder) deduped(key string, now time.Time) bool {
	t, ok := r.lastSeen[key]
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
		Schema:    1,
		Source:    r.cfg.Source,
		Version:   r.cfg.Version,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Time:      now.UTC().Format(time.RFC3339Nano),
		Site:      site,
		PanicType: truncate(Redact(fmt.Sprintf("%T", v), r.cfg.Home, r.cfg.SmindHome), 200),
		Stack:     Redact(string(stack), r.cfg.Home, r.cfg.SmindHome),
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

// marshalBounded marshals rep and, while it exceeds limit bytes, cuts the
// stack (keeping its head) and appends the truncation marker until it fits.
func marshalBounded(rep report, limit int) ([]byte, error) {
	data, err := json.Marshal(rep)
	if err != nil || len(data) <= limit {
		return data, err
	}
	base, keep := rep.Stack, len(rep.Stack)
	for {
		keep -= len(data) - limit + len(stackTruncated)
		if keep < 0 {
			keep = 0
		}
		rep.Stack = base[:keep] + stackTruncated
		data, err = json.Marshal(rep)
		if err != nil || len(data) <= limit || keep == 0 {
			return data, err
		}
	}
}

// dedupeFrame returns the first stack frame outside runtime, this package and
// the panic(...) frame itself (whose line carries the value's raw addresses):
// the frame that identifies where the panic happened. File and line rows are
// indented and skipped. Arguments are cut so addresses never reach the key.
func dedupeFrame(stack []byte) string {
	for _, line := range strings.Split(string(stack), "\n") {
		if line == "" || line[0] == '\t' {
			continue
		}
		if strings.HasPrefix(line, "goroutine ") || strings.HasPrefix(line, "created by ") || strings.HasPrefix(line, "runtime/") ||
			strings.HasPrefix(line, "runtime.") || strings.HasPrefix(line, "panic(") || strings.HasPrefix(line, "testing.") {
			continue
		}
		if isRecorderFrame(line) {
			continue
		}
		if i := strings.LastIndex(line, "("); i > 0 {
			line = line[:i]
		}
		return line
	}
	return ""
}

// isRecorderFrame reports whether a stack line is one of this package's own
// recovery entry points (not a function that merely lives in a package named
// crash, such as a test helper).
func isRecorderFrame(line string) bool {
	for _, fn := range [...]string{"internal/crash.Recover", "internal/crash.(*Recorder).", "internal/crash.RecordAndRepanic", "internal/crash.Go"} {
		if strings.Contains(line, fn) {
			return true
		}
	}
	return false
}

// truncate cuts s to at most limit bytes without splitting a rune.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	b := []byte(s[:limit])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// logErr swallows every error after logging the first one per process.
func logErr(err error) {
	logErrOnce.Do(func() { log.Printf("crash: could not record a report: %v", err) })
}
