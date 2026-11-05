package runs

import (
	"container/list"
	"fmt"
	"unsafe"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// DefaultHistoryBudget is the default cap on the estimated bytes of finished
// runs' event history the Registry keeps resident (config runs.historyBudgetMB).
// Go's live-heap target is ~2x live with GOGC=100, so 64 MB of history costs
// ~130 MB worst case on top of the runtime baseline. A running run's history is
// never counted or evicted; a finished run's history that is not resident is
// served complete from the store (docs/plans/active/daemon-memory.md).
const DefaultHistoryBudget int64 = 64 << 20

// histState tracks which finished runs have their history resident. Guarded by
// Registry.mu. lru holds *run, most recently used at the front.
type histState struct {
	lru        *list.List
	resident   int64
	budget     int64
	loads      int64 // store loads adopted into the cache
	evictions  int64 // finished runs whose history was dropped
	storeReads int64 // full or tail reads served from the store
}

// HistoryStats is the registry's view of run history memory, for `smind debug memory`.
type HistoryStats struct {
	RunsInMemory     int   `json:"runsInMemory"`
	RunningRuns      int   `json:"runningRuns"`
	ResidentRuns     int   `json:"residentRuns"`
	ResidentBytes    int64 `json:"residentBytes"`
	BudgetBytes      int64 `json:"budgetBytes"`
	RunningHistBytes int64 `json:"runningHistoryBytes"`
	Loads            int64 `json:"loads"`
	Evictions        int64 `json:"evictions"`
	StoreReads       int64 `json:"storeReads"`
}

// SetHistoryBudget sets the resident-history budget in bytes (<= 0 restores the
// default) and evicts down to it.
func (reg *Registry) SetHistoryBudget(bytes int64) {
	if bytes <= 0 {
		bytes = DefaultHistoryBudget
	}
	reg.mu.Lock()
	reg.hist.budget = bytes
	reg.enforceBudgetLocked()
	reg.mu.Unlock()
}

// HistoryStats snapshots the counters and the resident totals.
func (reg *Registry) HistoryStats() HistoryStats {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	st := HistoryStats{RunsInMemory: len(reg.runs), ResidentBytes: reg.hist.resident, BudgetBytes: reg.histBudgetLocked(),
		Loads: reg.hist.loads, Evictions: reg.hist.evictions, StoreReads: reg.hist.storeReads}
	for _, r := range reg.runs {
		r.mu.Lock()
		switch {
		case r.status == StatusRunning:
			st.RunningRuns++
			st.RunningHistBytes += r.histBytes
		case !r.histEvicted:
			st.ResidentRuns++
		}
		r.mu.Unlock()
	}
	return st
}

func (reg *Registry) histBudgetLocked() int64 {
	if reg.hist.budget <= 0 {
		return DefaultHistoryBudget
	}
	return reg.hist.budget
}

// eventBytes estimates the heap an event keeps alive: its struct plus every
// string, raw JSON and slice it points to. An estimate, not an exact size.
func eventBytes(e Event) int64 {
	n := int64(unsafe.Sizeof(e))
	n += int64(len(e.Text) + len(e.StopReason) + len(e.PermissionRequestID) + len(e.PermissionSummary) +
		len(e.PermissionOptionID) + len(e.PermissionRuleID) + len(e.ToolCallID) + len(e.ToolName) +
		len(e.ToolTitle) + len(e.ToolStatus) + len(e.ToolInput) + len(e.ToolResult) + len(e.RawKind) + len(e.RawPayload))
	for _, o := range e.PermissionOptions {
		n += 64 + int64(len(o.ID)+len(o.Label))
	}
	if d := e.PermissionDetail; d != nil {
		n += 256 + int64(len(d.Path)+len(d.Preview)+len(d.Diff)+len(d.Command)+len(d.PolicyCommand))
		for _, b := range d.RevealBodies {
			n += 16 + int64(len(b))
		}
	}
	if e.Usage != nil {
		n += 128
	}
	for _, m := range e.McpServers {
		n += 64 + int64(len(m.Name)+len(m.Transport)+len(m.Status)+len(m.Reason))
	}
	return n
}

func historyBytes(h []Event) int64 {
	var n int64
	for _, e := range h {
		n += eventBytes(e)
	}
	return n
}

// admitFinished is called once a run has finished: its history becomes
// evictable and the budget is enforced. Called from retain (reg.mu held).
func (reg *Registry) admitFinishedLocked(r *run) {
	r.mu.Lock()
	// Raw is the backend-native value, never persisted: a run reloaded from the
	// store has none, so drop it here and a resident run reads the same.
	for i := range r.history {
		r.history[i].Raw = nil
	}
	r.histBytes = historyBytes(r.history)
	size := r.histBytes
	r.mu.Unlock()
	reg.pushHistLocked(r, size)
	reg.enforceBudgetLocked()
}

// pushHistLocked puts r at the front of the LRU with size bytes resident.
func (reg *Registry) pushHistLocked(r *run, size int64) {
	if reg.hist.lru == nil {
		reg.hist.lru = list.New()
	}
	r.mu.Lock()
	if r.histElem != nil {
		reg.hist.lru.Remove(r.histElem)
		reg.hist.resident -= r.histBytes
	}
	r.histBytes = size
	r.histElem = reg.hist.lru.PushFront(r)
	r.mu.Unlock()
	reg.hist.resident += size
}

// dropHistLocked forgets r's resident history (evicted, or r left the registry).
func (reg *Registry) dropHistLocked(r *run, evicted bool) {
	r.mu.Lock()
	if r.histElem != nil {
		reg.hist.lru.Remove(r.histElem)
		reg.hist.resident -= r.histBytes
		r.histElem = nil
		if evicted {
			r.history = nil
			r.histEvicted = true
			reg.hist.evictions++
		}
		r.histBytes = 0
	}
	r.mu.Unlock()
}

// enforceBudgetLocked drops least-recently-used finished histories until the
// resident total fits. Running runs are not in the LRU, so they are never
// touched.
func (reg *Registry) enforceBudgetLocked() {
	if reg.hist.lru == nil {
		return
	}
	for reg.hist.resident > reg.histBudgetLocked() {
		back := reg.hist.lru.Back()
		if back == nil {
			return
		}
		reg.dropHistLocked(back.Value.(*run), true)
	}
}

func (reg *Registry) touchHist(r *run) {
	reg.mu.Lock()
	r.mu.Lock()
	if r.histElem != nil {
		reg.hist.lru.MoveToFront(r.histElem)
	}
	r.mu.Unlock()
	reg.mu.Unlock()
}

// loadStoredEvents decodes every persisted event of runID one row at a time.
func loadStoredEvents(st *store.Store, runID string) ([]Event, int64, error) {
	var hist []Event
	var size int64
	err := st.StreamRunEvents(runID, func(se store.RunEvent) error {
		e, err := decodeEvent(se.EventData)
		if err != nil {
			return fmt.Errorf("decode event seq %d: %w", se.Seq, err)
		}
		hist = append(hist, e)
		size += eventBytes(e)
		return nil
	})
	return hist, size, err
}

// finishedHistory returns a copy of a finished run's complete history: from
// memory when resident, else streamed from the store (and cached when it fits
// the budget). Concurrent callers for one run share a single store read.
func (reg *Registry) finishedHistory(r *run) ([]Event, error) {
	for {
		r.mu.Lock()
		if at := r.histPrunedAt; at != nil {
			stop := r.stopReason
			r.mu.Unlock()
			return reg.prunedSummary(r.id, stop, *at), nil
		}
		if !r.histEvicted {
			h := append([]Event(nil), r.history...)
			r.mu.Unlock()
			reg.touchHist(r)
			return h, nil
		}
		if wait := r.loadCh; wait != nil {
			r.mu.Unlock()
			<-wait
			continue
		}
		done := make(chan struct{})
		r.loadCh = done
		r.mu.Unlock()

		if reg.histLoadHook != nil {
			reg.histLoadHook()
		}
		hist, size, err := loadStoredEvents(reg.st, r.id)
		reg.mu.Lock()
		reg.hist.storeReads++
		r.mu.Lock()
		r.loadCh = nil
		if err == nil && r.histEvicted && r.histPrunedAt == nil && reg.runs[r.id] == r && size <= reg.histBudgetLocked() {
			r.history = hist
			r.histEvicted = false
			r.mu.Unlock()
			reg.hist.loads++
			reg.pushHistLocked(r, size)
			reg.enforceBudgetLocked()
		} else {
			r.mu.Unlock()
		}
		reg.mu.Unlock()
		close(done)
		if err != nil {
			return nil, err
		}
		return append([]Event(nil), hist...), nil
	}
}

// TailView is the last events of a run plus what the log elider needs from
// before them: the events that carry a tail tool call's input.
type TailView struct {
	Events []Event
	Prime  []Event
	Status RunStatus
}

// Tail returns the last n events of runID (all when n <= 0 or n covers the run)
// without loading a non-resident finished run: the store serves the tail by an
// index range scan. Prime holds, in order, the earlier events whose tool call
// id appears in the tail and that carry an input, so the elider converts the
// tail exactly as it would inside the full history.
func (reg *Registry) Tail(runID string, n int) (TailView, error) {
	r, err := reg.get(runID)
	if err != nil {
		return reg.storedTail(runID, n)
	}
	r.mu.Lock()
	if r.status == StatusRunning || !r.histEvicted {
		view := tailOf(r.history, n)
		status := r.statusLocked()
		r.mu.Unlock()
		if status.Status != StatusRunning {
			reg.touchHist(r)
			view.Status = reg.withUsage(status)
		} else {
			view.Status = status
		}
		return view, nil
	}
	status := r.statusLocked()
	pruned := r.histPrunedAt
	r.mu.Unlock()
	if pruned != nil {
		view := tailOf(reg.prunedSummary(runID, status.StopReason, *pruned), n)
		view.Status = reg.withUsage(status)
		return view, nil
	}
	view, err := reg.storeTail(runID, n)
	if err != nil {
		return TailView{}, err
	}
	view.Status = reg.withUsage(status)
	return view, nil
}

func (reg *Registry) withUsage(status RunStatus) RunStatus {
	if status.Usage == nil && status.Status != StatusRunning {
		if u, err := reg.st.GetRunUsage(status.ID); err == nil {
			status.Usage = runUsageFromStore(u)
		}
	}
	return status
}

// tailOf slices an in-memory history. The result shares nothing mutable with h.
func tailOf(h []Event, n int) TailView {
	if n <= 0 || len(h) <= n {
		return TailView{Events: append([]Event(nil), h...)}
	}
	start := len(h) - n
	tail := append([]Event(nil), h[start:]...)
	ids := tailMissingInputIDs(tail)
	var prime []Event
	for _, e := range h[:start] {
		if e.Type == taskrunner.EventTypeToolCall && len(e.ToolInput) > 0 && ids[e.ToolCallID] {
			prime = append(prime, e)
		}
	}
	return TailView{Events: tail, Prime: prime}
}

// tailMissingInputIDs are the tool call ids of tail events whose input the tail
// itself does not carry for them (an event that has it elides on its own).
func tailMissingInputIDs(tail []Event) map[string]bool {
	ids := map[string]bool{}
	for _, e := range tail {
		if e.Type == taskrunner.EventTypeToolCall && e.ToolCallID != "" {
			ids[e.ToolCallID] = true
		}
	}
	return ids
}

// maxPrimeIDs caps the ids of one ToolCallInputEvents query (SQLite bound variables).
const maxPrimeIDs = 500

func (reg *Registry) storeTail(runID string, n int) (TailView, error) {
	reg.mu.Lock()
	reg.hist.storeReads++
	reg.mu.Unlock()
	if n <= 0 {
		hist, _, err := loadStoredEvents(reg.st, runID)
		return TailView{Events: hist}, err
	}
	rows, err := reg.st.TailRunEvents(runID, n)
	if err != nil {
		return TailView{}, err
	}
	tail := make([]Event, 0, len(rows))
	for _, se := range rows {
		e, err := decodeEvent(se.EventData)
		if err != nil {
			return TailView{}, fmt.Errorf("decode event seq %d: %w", se.Seq, err)
		}
		tail = append(tail, e)
	}
	view := TailView{Events: tail}
	if len(rows) < n || len(rows) == 0 {
		return view, nil
	}
	var ids []string
	for id := range tailMissingInputIDs(tail) {
		ids = append(ids, id)
	}
	if len(ids) > maxPrimeIDs {
		// Too many distinct calls for one IN list: convert from the whole run.
		hist, _, err := loadStoredEvents(reg.st, runID)
		return tailOf(hist, n), err
	}
	prior, err := reg.st.ToolCallInputEvents(runID, ids, rows[0].Seq)
	if err != nil {
		return TailView{}, err
	}
	for _, se := range prior {
		e, err := decodeEvent(se.EventData)
		if err != nil {
			return TailView{}, fmt.Errorf("decode event seq %d: %w", se.Seq, err)
		}
		view.Prime = append(view.Prime, e)
	}
	return view, nil
}

// storedTail is Tail for a run the Registry no longer lists (past the metadata
// cap): same rules as storedHistory for what may be read.
func (reg *Registry) storedTail(runID string, n int) (TailView, error) {
	row, err := reg.storedReadable(runID)
	if err != nil {
		return TailView{}, err
	}
	meta := rehydrateMeta(reg.st, row)
	status := meta.statusLocked()
	var view TailView
	if row.HistoryPrunedAt != nil {
		view = tailOf(reg.prunedSummary(runID, row.StopReason, *row.HistoryPrunedAt), n)
	} else if view, err = reg.storeTail(runID, n); err != nil {
		return TailView{}, err
	}
	if u, err := reg.st.GetRunUsage(runID); err == nil {
		status.Usage = runUsageFromStore(u)
	}
	view.Status = status
	return view, nil
}

// trailingTextEvents returns a suffix of runID's history long enough for
// trailingAgentText / taskrunner.FinalText to give the same answer as on the
// full history: a resident run answers from memory, a non-resident one grows a
// tail window from the store until the text run is bounded by an earlier event
// or the window is the whole run. finalText selects FinalText's skip rule (any
// non-text event) over trailingAgentText's (bookkeeping events only).
func (reg *Registry) trailingTextEvents(runID string, finalText bool) ([]Event, error) {
	r, err := reg.get(runID)
	if err == nil {
		r.mu.Lock()
		if at := r.histPrunedAt; at != nil {
			stop := r.stopReason
			r.mu.Unlock()
			return reg.prunedSummary(runID, stop, *at), nil
		}
		if r.status == StatusRunning || !r.histEvicted {
			h := append([]Event(nil), r.history...)
			r.mu.Unlock()
			return h, nil
		}
		r.mu.Unlock()
	} else if row, serr := reg.storedReadable(runID); serr != nil {
		return nil, serr
	} else if row.HistoryPrunedAt != nil {
		return reg.prunedSummary(runID, row.StopReason, *row.HistoryPrunedAt), nil
	}
	for n := 64; ; n *= 8 {
		view, err := reg.storeTail(runID, n)
		if err != nil {
			return nil, err
		}
		if len(view.Events) < n || textRunBounded(view.Events, finalText) {
			return view.Events, nil
		}
	}
}

// textRunBounded reports whether the walk trailingAgentText / FinalText does
// back from the end of w stops on an event inside w (so more history before w
// could not change the answer).
func textRunBounded(w []Event, finalText bool) bool {
	end := len(w)
	for end > 0 {
		t := w[end-1].Type
		if finalText && t != taskrunner.EventTypeText || !finalText && isBookkeepingEvent(t) {
			end--
			continue
		}
		break
	}
	i := end
	for i > 0 && w[i-1].Type == taskrunner.EventTypeText {
		i--
	}
	return i > 0
}

// LastAgentText is trailingAgentText of runID's history, without loading a
// non-resident run.
func (reg *Registry) LastAgentText(runID string) (string, error) {
	h, err := reg.trailingTextEvents(runID, false)
	if err != nil {
		return "", err
	}
	return trailingAgentText(h), nil
}

// FinalAgentText is taskrunner.FinalText of runID's history, without loading a
// non-resident run.
func (reg *Registry) FinalAgentText(runID string) (string, error) {
	h, err := reg.trailingTextEvents(runID, true)
	if err != nil {
		return "", err
	}
	return taskrunner.FinalText(h), nil
}

// toolCallInput returns the last non-empty ToolInput recorded for toolCallID in
// runID: from memory for a running or resident run, else one indexed store
// query (json_extract over the run's rows), without loading the run.
func (reg *Registry) toolCallInput(runID, toolCallID string) ([]byte, error) {
	r, err := reg.get(runID)
	if err == nil {
		r.mu.Lock()
		if r.histPrunedAt != nil {
			r.mu.Unlock()
			return nil, ErrHistoryPruned
		}
		if r.status == StatusRunning || !r.histEvicted {
			input := lastToolInput(r.history, toolCallID)
			r.mu.Unlock()
			return input, nil
		}
		r.mu.Unlock()
	} else if row, serr := reg.storedReadable(runID); serr != nil {
		return nil, serr
	} else if row.HistoryPrunedAt != nil {
		return nil, ErrHistoryPruned
	}
	reg.mu.Lock()
	reg.hist.storeReads++
	reg.mu.Unlock()
	rows, err := reg.st.ToolCallInputEvents(runID, []string{toolCallID}, 1<<62)
	if err != nil {
		return nil, err
	}
	var input []byte
	truncated := false
	for _, se := range rows {
		e, err := decodeEvent(se.EventData)
		if err != nil {
			return nil, fmt.Errorf("decode event seq %d: %w", se.Seq, err)
		}
		if e.Type == taskrunner.EventTypeToolCall && e.ToolCallID == toolCallID && len(e.ToolInput) > 0 {
			input, truncated = e.ToolInput, se.Truncated
		}
	}
	if truncated {
		return nil, ErrHeredocUnavailable
	}
	return input, nil
}

// lastToolInput: a later update may omit the input, so keep the last one that has it.
func lastToolInput(h []Event, toolCallID string) []byte {
	var input []byte
	for _, e := range h {
		if e.Type == taskrunner.EventTypeToolCall && e.ToolCallID == toolCallID && len(e.ToolInput) > 0 {
			input = e.ToolInput
		}
	}
	return input
}

// storedReadable reports ErrNotFound for ids that may not be read by history
// consumers (unknown, running, system runs), nil otherwise.
func (reg *Registry) storedReadable(runID string) (store.Run, error) {
	row, err := reg.st.GetRun(runID)
	if err != nil || row.Status == string(StatusRunning) || row.Purpose != "" && row.Purpose != "user" {
		return store.Run{}, ErrNotFound
	}
	return row, nil
}
