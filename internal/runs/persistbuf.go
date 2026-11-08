package runs

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Write-time coalescing of persisted chunk rows (ADR-0053, H3).
//
// A streamed provider emits one text or thinking chunk per few tokens, and the
// store used to keep one run_events row per chunk: on the real database 98% of
// the rows were chunks of about 6 bytes, each costing about 174 bytes of table
// and index. persistBuf merges the PERSISTED rows of consecutive chunks of one
// kind into one row. Nothing else changes: r.history and every subscriber still
// receive every chunk, one event each, exactly as before.
//
// A buffer is flushed (one row written) when
//   - a non-chunk event arrives (a boundary: tool call, permission, usage, done...);
//   - the chunk kind changes (text <-> thinking), or its seq is not the one right
//     after the buffer's last (another goroutine recorded an event in between);
//   - adding the chunk would pass persistChunkMax bytes;
//   - persistIdle passes without a chunk (a timer);
//   - the run finishes (finish, which is also what CloseAll waits for).
//
// The merged row keeps the FIRST chunk's seq; the seqs the other chunks drew
// are gaps (readers tolerate them; see run-events-retention.md decision 3).
// The row is written once, at flush, so no row is ever updated.
//
// Durability: chunks still in the buffer when the process dies without a
// shutdown (kill -9, power loss) are lost: at most persistIdle (250 ms) of
// streaming, or persistChunkMax (4 KiB) of text, whichever is smaller, per
// running run. A normal stop, a Stop, an error and a daemon shutdown all go
// through finish and lose nothing.
const (
	persistChunkMax = 4 << 10
	persistIdle     = 250 * time.Millisecond
)

type persistBuf struct {
	mu      sync.Mutex
	open    bool
	typ     taskrunner.EventType
	seq     int64 // seq of the first chunk: the row's seq
	lastSeq int64 // seq of the latest chunk merged
	text    strings.Builder
	last    time.Time
	timer   *time.Timer
}

// persistedFieldsCovered is the number of persistedEvent fields chunkOnly
// accounts for; TestChunkOnlyCoversPersistedFields fails when a field is added
// to persistedEvent without deciding what it means for coalescing.
const persistedFieldsCovered = 23

// chunkOnly reports whether e is a plain text or thinking chunk: only its type
// and text are set, so merging it with its neighbours loses nothing.
func chunkOnly(e Event) bool {
	if e.Type != taskrunner.EventTypeText && e.Type != taskrunner.EventTypeThinking {
		return false
	}
	return e.StopReason == "" && e.PermissionRequestID == "" && e.PermissionSummary == "" &&
		len(e.PermissionOptions) == 0 && e.PermissionOptionID == "" && e.PermissionResolution == "" &&
		e.PermissionRuleID == "" && e.PermissionDetail == nil && e.ToolCallID == "" && e.ToolName == "" &&
		e.ToolTitle == "" && e.ToolStatus == "" && len(e.ToolInput) == 0 && len(e.ToolResult) == 0 &&
		e.RawKind == "" && len(e.RawPayload) == 0 && e.Usage == nil && len(e.McpServers) == 0 && e.Question == nil
}

// (V and Skel, the skeleton fields, are not on Event: a chunk is never skeleton.)

var persistedEventFields = reflect.TypeOf(persistedEvent{}).NumField()

// persist writes e, which drew seq, to the store: a plain chunk joins the run's
// open buffer, anything else flushes the buffer first and is written as is.
// Best-effort like the append it replaces.
func (reg *Registry) persist(r *run, seq int64, e Event) {
	b := &r.pbuf
	b.mu.Lock()
	defer b.mu.Unlock()
	if !chunkOnly(e) {
		reg.flushLocked(r, b)
		if data, err := encodeEvent(e); err == nil {
			_, _ = reg.st.AppendRunEvent(r.id, seq, data)
		}
		return
	}
	if b.open && (b.typ != e.Type || seq != b.lastSeq+1 || b.text.Len()+len(e.Text) > persistChunkMax) {
		reg.flushLocked(r, b)
	}
	if !b.open {
		b.open, b.typ, b.seq = true, e.Type, seq
		b.text.Reset()
		if b.timer == nil {
			b.timer = time.AfterFunc(persistIdle, func() { reg.idleFlush(r) })
		} else {
			b.timer.Reset(persistIdle)
		}
	}
	b.lastSeq = seq
	b.text.WriteString(e.Text)
	b.last = time.Now()
}

// flushLocked writes the open buffer as one row. b.mu is held.
func (reg *Registry) flushLocked(r *run, b *persistBuf) {
	if !b.open {
		return
	}
	b.open = false
	if b.timer != nil {
		b.timer.Stop()
	}
	if data, err := encodeEvent(Event{Type: b.typ, Text: b.text.String()}); err == nil {
		_, _ = reg.st.AppendRunEvent(r.id, b.seq, data)
	}
	b.text.Reset()
}

// idleFlush is the buffer's timer: flush once persistIdle has passed since the
// latest chunk, otherwise wait out the rest.
func (reg *Registry) idleFlush(r *run) {
	b := &r.pbuf
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return
	}
	if rest := persistIdle - time.Since(b.last); rest > 0 {
		b.timer.Reset(rest)
		return
	}
	reg.flushLocked(r, b)
}

// flushPersist writes whatever the run still buffers. finish calls it before the
// terminal status is stored, so a reader that sees the run end sees every row.
func (reg *Registry) flushPersist(r *run) {
	r.pbuf.mu.Lock()
	reg.flushLocked(r, &r.pbuf)
	r.pbuf.mu.Unlock()
}
