package host

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// LogEntry is one line of a plugin's log: what it printed on stderr, or a
// `[smind]` message from the host (start, ready, exit, protocol error).
type LogEntry struct {
	Time   time.Time `json:"t"`
	Stream string    `json:"stream"` // "stderr" | "host"
	Text   string    `json:"text"`
}

// LogLimits bound a plugin's log. Whatever a plugin prints, memory and CPU
// stay inside these numbers.
type LogLimits struct {
	MaxLines     int // ring size
	MaxBytes     int // total text held
	MaxLineBytes int // a stored line is cut to this
	ReadLine     int // the part of one line that is even looked at (redaction cost)
	LinesPerSec  int // beyond this, lines are counted and dropped
}

// Log is a bounded per-plugin ring buffer. Every line is stripped of control
// characters (so a plugin cannot drive a terminal that prints its log) and
// passed through redact BEFORE it is stored, so the ring never holds a secret.
type Log struct {
	lim    LogLimits
	redact func(string) string
	now    func() time.Time

	mu      sync.Mutex
	entries []LogEntry
	bytes   int
	dropped int       // evicted from the ring or rate-limited since creation
	winAt   time.Time // current rate window
	winN    int
	winDrop int
}

func newLog(lim LogLimits, redact func(string) string, now func() time.Time) *Log {
	if redact == nil {
		redact = func(s string) string { return s }
	}
	return &Log{lim: lim, redact: redact, now: now}
}

// Hostf records a host message with the `[smind]` prefix. Host messages are
// not rate limited (the host writes few) but are cut and redacted like any line.
func (l *Log) Hostf(format string, a ...any) {
	l.add("host", "[smind] "+fmt.Sprintf(format, a...), false)
}

func (l *Log) add(stream, text string, limited bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if limited {
		l.roll(now)
		if l.winN >= l.lim.LinesPerSec {
			l.winDrop++
			return
		}
		l.winN++
	}
	text = clean(text)
	text = l.redact(text)
	if len(text) > l.lim.MaxLineBytes {
		text = strings.ToValidUTF8(text[:l.lim.MaxLineBytes], "") + "...[truncated]"
	}
	l.push(LogEntry{Time: now, Stream: stream, Text: text})
}

// roll closes the rate window once a second has passed, storing one summary
// line for what it dropped; mu held.
func (l *Log) roll(now time.Time) {
	if now.Sub(l.winAt) < time.Second {
		return
	}
	if l.winDrop > 0 {
		l.dropped += l.winDrop
		l.push(LogEntry{Time: now, Stream: "host", Text: fmt.Sprintf("[smind] %d stderr lines dropped (over %d lines/s)", l.winDrop, l.lim.LinesPerSec)})
	}
	l.winAt, l.winN, l.winDrop = now, 0, 0
}

// push appends and evicts the oldest entries past either cap; mu held.
func (l *Log) push(e LogEntry) {
	l.entries = append(l.entries, e)
	l.bytes += len(e.Text)
	for len(l.entries) > l.lim.MaxLines || l.bytes > l.lim.MaxBytes {
		if len(l.entries) == 0 {
			break
		}
		l.bytes -= len(l.entries[0].Text)
		l.entries = l.entries[1:]
		l.dropped++
	}
}

// Lines returns up to limit of the newest entries, oldest first (limit <= 0:
// all held).
func (l *Log) Lines(limit int) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll(l.now())
	es := l.entries
	if limit > 0 && len(es) > limit {
		es = es[len(es)-limit:]
	}
	return append([]LogEntry(nil), es...)
}

// Stats reports what the ring holds and how much was dropped.
func (l *Log) Stats() (lines, bytes, dropped int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll(l.now())
	return len(l.entries), l.bytes, l.dropped
}

// clean makes a line printable: invalid UTF-8 and C0/C1/DEL controls (ESC
// included) become '?', tab becomes a space.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "?")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == 0x2028, r == 0x2029:
			return '?'
		}
		return r
	}, s)
}

// writer returns the io.Writer for a process's stderr: it splits lines and
// never buffers more than ReadLine bytes of one (the rest of an over-long
// line is skipped, not stored).
func (l *Log) writer() *lineWriter { return &lineWriter{l: l} }

type lineWriter struct {
	l        *Log
	mu       sync.Mutex
	buf      []byte
	skipping bool
	cut      bool
}

var _ io.Writer = (*lineWriter)(nil)

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			w.take(p)
			break
		}
		w.take(p[:i])
		w.flush()
		p = p[i+1:]
	}
	return n, nil
}

func (w *lineWriter) take(b []byte) {
	if w.skipping {
		return
	}
	room := w.l.lim.ReadLine - len(w.buf)
	if len(b) > room {
		w.buf = append(w.buf, b[:room]...)
		w.skipping, w.cut = true, true
		return
	}
	w.buf = append(w.buf, b...)
}

func (w *lineWriter) flush() {
	line := strings.TrimSuffix(string(w.buf), "\r")
	if w.cut {
		line = strings.ToValidUTF8(line, "") + "...[truncated]"
	}
	w.buf, w.skipping, w.cut = w.buf[:0], false, false
	if line != "" {
		w.l.add("stderr", line, true)
	}
}

// Flush stores a final line that had no newline.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
}
