package runstats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// Constants of the models below. They mirror the ADR-0053 starting values; the
// report evaluates a grid around them so the frozen numbers rest on data.
const (
	// ChunkFlushBytes and ChunkIdle are the PR1 write-time coalescing limits.
	ChunkFlushBytes = 4 << 10
	ChunkIdle       = 250 * time.Millisecond
	// AnswerRowCap is the skeleton's cap on one coalesced answer row.
	AnswerRowCap = 32 << 10
	// ToolRowFixed is the modelled size of a skeleton tool row without its
	// summaries; DescriptorBytes is one excerpt descriptor; ThinkingRowBytes is
	// the one row that replaces a run of thinking chunks (H1).
	ToolRowFixed     = 250
	DescriptorBytes  = 90
	ThinkingRowBytes = 120
	// ArgsSummaryCap and ResultSummaryCap are the skeleton summary caps.
	ArgsSummaryCap   = 512
	ResultSummaryCap = 200
)

// Grid of threshold choices evaluated by Report.Thresholds.
var (
	inlineGrid = []int{256, 512, 1024}
	wholeGrid  = []int{4 << 10, 8 << 10, 16 << 10, 32 << 10}
)

// Dist is a size distribution: counts and bytes only.
type Dist struct {
	N   int   `json:"n"`
	Sum int64 `json:"sum"`
	P50 int   `json:"p50"`
	P90 int   `json:"p90"`
	P99 int   `json:"p99"`
	Max int   `json:"max"`
}

// Bucket is a rows/bytes pair.
type Bucket struct {
	Rows  int64 `json:"rows"`
	Bytes int64 `json:"bytes"`
}

// TypeStats is one event type's rows and event_data size distribution.
type TypeStats struct {
	Type string `json:"type"`
	Bucket
	Size Dist `json:"size"`
}

// ProviderStats is one provider's share of the database.
type ProviderStats struct {
	Provider    string `json:"provider"`
	Runs        int    `json:"runs"`
	Rows        int64  `json:"rows"`
	Bytes       int64  `json:"bytes"`
	RowsPerRun  Dist   `json:"rowsPerRun"`
	BytesPerRun Dist   `json:"bytesPerRun"`
	TextChunk   Dist   `json:"textChunkBytes"`
	ToolInput   Dist   `json:"toolInputBytes"`
	ToolResult  Dist   `json:"toolResultBytes"`
}

// ToolStats is one tool name's payload distributions.
type ToolStats struct {
	Tool   string `json:"tool"`
	Calls  int    `json:"calls"`
	Input  Dist   `json:"inputBytes"`
	Result Dist   `json:"resultBytes"`
}

// Coalescing is the estimated effect of write-time chunk coalescing (PR1).
type Coalescing struct {
	ChunkRowsNow       int64 `json:"chunkRowsNow"`
	ChunkBytesNow      int64 `json:"chunkBytesNow"`
	RowsBoundaryAnd4K  int64 `json:"rowsBoundaryAnd4KiB"`
	BytesBoundaryAnd4K int64 `json:"bytesBoundaryAnd4KiB"`
	RowsWithIdle       int64 `json:"rowsWithIdle250ms"`
	BytesWithIdle      int64 `json:"bytesWithIdle250ms"`
	// SavedBytesWithIdle includes the per-row table and index overhead.
	SavedBytesWithIdle int64 `json:"savedBytesWithIdle"`
	// AnswerRun is the size of one contiguous run of text chunks (no 4 KiB cut).
	AnswerRun Dist `json:"answerRunBytes"`
}

// Threshold is the payload bytes kept under one (inline, whole) choice.
type Threshold struct {
	InlineCap         int     `json:"inlineCapBytes"`
	WholeCap          int     `json:"wholeCapBytes"`
	PayloadsWhole     float64 `json:"payloadsKeptWholePct"`
	InlineBytes       int64   `json:"inlineBytes"`
	ExcerptTierBytes  int64   `json:"excerptTierBytes"`
	DescriptorBytes   int64   `json:"descriptorBytes"`
	SkeletonOnlyBytes int64   `json:"payloadBytesAfterExcerptPrune"`
	WithExcerptBytes  int64   `json:"payloadBytesWithExcerpts"`
	SavedPct          float64 `json:"payloadSavedPctWithExcerpts"`
}

// Estimate models the whole database under each storage form.
type Estimate struct {
	CurrentBytes              int64  `json:"currentBytes"`
	AfterCoalescingBytes      int64  `json:"afterCoalescingBytes"`
	SkeletonWithExcerptsBytes int64  `json:"skeletonWithExcerptsBytes"`
	SkeletonOnlyBytes         int64  `json:"skeletonOnlyBytes"`
	ThinkingBytesNow          int64  `json:"thinkingBytesNow"`
	Assumptions               string `json:"assumptions"`
}

// DB is the file's page accounting.
type DB struct {
	FileBytes         int64  `json:"fileBytes"`
	FreelistBytes     int64  `json:"freelistBytes"`
	RunEventsTableB   int64  `json:"runEventsTableBytes"`
	RunEventsIndexB   int64  `json:"runEventsIndexBytes"`
	OtherBytes        int64  `json:"otherBytes"`
	RowOverheadBytes  int64  `json:"rowOverheadBytes"`
	RowOverheadSource string `json:"rowOverheadSource"`
}

// Report is the whole measurement. It carries no text and no run ids.
type Report struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	DB          DB                `json:"db"`
	Runs        int               `json:"runs"`
	Rows        int64             `json:"rows"`
	EventBytes  int64             `json:"eventDataBytes"`
	RowsPerRun  Dist              `json:"rowsPerRun"`
	BytesPerRun Dist              `json:"bytesPerRun"`
	ByType      []TypeStats       `json:"byType"`
	ByProvider  []ProviderStats   `json:"byProvider"`
	ByAge       map[string]Bucket `json:"byRunAge"`
	ByTool      []ToolStats       `json:"byTool"`
	ToolCalls   int64             `json:"toolCalls"`
	Payload     Dist              `json:"payloadBytes"`
	Coalescing  Coalescing        `json:"coalescing"`
	Thresholds  []Threshold       `json:"thresholds"`
	Estimate    Estimate          `json:"estimate"`
}

var typeNames = map[int]string{
	int(taskrunner.EventTypeText):                  "text",
	int(taskrunner.EventTypeDone):                  "done",
	int(taskrunner.EventTypePermissionRequest):     "permission_request",
	int(taskrunner.EventTypePermissionResolved):    "permission_resolved",
	int(taskrunner.EventTypeUserMessage):           "user_message",
	int(taskrunner.EventTypeThinking):              "thinking",
	int(taskrunner.EventTypeToolCall):              "tool_call",
	int(taskrunner.EventTypeRaw):                   "raw",
	int(taskrunner.EventTypeSessionNote):           "session_note",
	int(taskrunner.EventTypePermissionModeChanged): "permission_mode_changed",
	int(taskrunner.EventTypeUsage):                 "usage",
	int(taskrunner.EventTypeMcpServers):            "mcp_servers",
	int(taskrunner.EventTypeRateLimit):             "rate_limit",
}

func typeName(t int) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("unknown_%d", t)
}

var labelRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,40}$`)

// label returns s when it is a short identifier, else "other": a tool or
// provider name is the only free-form string that may reach the output.
func label(s string) string {
	if labelRE.MatchString(s) {
		return s
	}
	return "other"
}

// row is the subset of a persisted event the measurement reads. The payload
// fields stay raw: only their encoded length is used, and nothing is printed.
type row struct {
	Type       int             `json:"type"`
	Text       json.RawMessage `json:"text"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	ToolInput  json.RawMessage `json:"toolInput"`
	ToolResult json.RawMessage `json:"toolResult"`
	RawPayload json.RawMessage `json:"rawPayload"`
	PermDetail json.RawMessage `json:"permissionDetail"`
}

type runInfo struct {
	provider string
	started  time.Time
}

// coBuf is one open coalescing buffer: contiguous chunks of one type.
type coBuf struct {
	open           bool
	typ, text, env int
}

func (b *coBuf) add(typ, text, env int, split bool, out *[2]int64) {
	if b.open && (split || b.typ != typ || b.text+text > ChunkFlushBytes) {
		b.flush(out)
	}
	if !b.open {
		b.open, b.typ, b.text, b.env = true, typ, 0, env
	}
	b.text += text
}

func (b *coBuf) flush(out *[2]int64) {
	if b.open {
		out[0]++
		out[1] += int64(b.env + b.text)
		b.open = false
	}
}

type runAcc struct {
	provider    string
	rows, bytes int64
	calls       map[uint64]struct{}
	bound, idle coBuf
	lastAt      time.Time
	runText     int // open contiguous text run (skeleton answer row)
	runEnv      int
	thinkOpen   bool
}

type collector struct {
	types      map[int]*typeAcc
	providers  map[string]*provAcc
	tools      map[string]*toolAcc
	age        map[string]Bucket
	rowsPerRun []int32
	bytesRun   []int32
	payloads   []int32
	answerRuns []int32

	chunkRows, chunkBytes int64
	bound, idle           [2]int64
	toolCalls             int64
	thinkBytes            int64

	// skeleton model
	otherRows, otherBytes   int64
	answerRows, answerBytes int64
	toolRows, toolBytes     int64
	excerptRows, excerptB   int64
}

type typeAcc struct {
	rows, bytes int64
	sizes       []int32
}

type provAcc struct {
	runs                    int
	rows, bytes             int64
	rowsPerRun, bytesPerRun []int32
	chunk, input, result    []int32
}

type toolAcc struct {
	calls         int
	bytes         int64
	input, result []int32
}

// Compute streams run_events of db once, in id order (a sequential table scan),
// and returns the report. It reads only; the caller owns db. now fixes the age
// buckets.
func Compute(ctx context.Context, db *sql.DB, path string, now time.Time) (*Report, error) {
	runs, err := loadRuns(ctx, db)
	if err != nil {
		return nil, err
	}
	c := &collector{
		types:     map[int]*typeAcc{},
		providers: map[string]*provAcc{},
		tools:     map[string]*toolAcc{},
		age:       map[string]Bucket{},
	}
	acc := map[string]*runAcc{}
	rows, err := db.QueryContext(ctx, `SELECT run_id, event_data, created_at FROM run_events ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read run_events: %w", err)
	}
	defer rows.Close()
	var total, nrows int64
	for rows.Next() {
		var runID, data string
		var created any
		if err := rows.Scan(&runID, &data, &created); err != nil {
			return nil, fmt.Errorf("scan run_events: %w", err)
		}
		ra := acc[runID]
		if ra == nil {
			ra = &runAcc{provider: providerLabel(runs[runID].provider), calls: map[uint64]struct{}{}}
			acc[runID] = ra
		}
		c.observe(ra, data, toTime(created))
		total += int64(len(data))
		nrows++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, ra := range acc {
		c.closeRun(ra)
		c.finishRun(ra, runs[id], now)
	}
	return c.report(ctx, db, path, now, nrows, total, len(runs))
}

func providerLabel(p string) string {
	if p == "" {
		return "unknown"
	}
	return label(p)
}

func loadRuns(ctx context.Context, db *sql.DB) (map[string]runInfo, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, provider, started_at FROM runs`)
	if err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}
	defer rows.Close()
	out := map[string]runInfo{}
	for rows.Next() {
		var id, provider string
		var started any
		if err := rows.Scan(&id, &provider, &started); err != nil {
			return nil, err
		}
		out[id] = runInfo{provider: provider, started: toTime(started)}
	}
	return out, rows.Err()
}

func toTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05"} {
			if p, err := time.Parse(layout, t); err == nil {
				return p
			}
		}
	}
	return time.Time{}
}

func (c *collector) typ(t int) *typeAcc {
	if a := c.types[t]; a != nil {
		return a
	}
	a := &typeAcc{}
	c.types[t] = a
	return a
}

func (c *collector) prov(n string) *provAcc {
	if a := c.providers[n]; a != nil {
		return a
	}
	a := &provAcc{}
	c.providers[n] = a
	return a
}

func (c *collector) tool(n string) *toolAcc {
	if a := c.tools[n]; a != nil {
		return a
	}
	a := &toolAcc{}
	c.tools[n] = a
	return a
}

func nonEmpty(r json.RawMessage) int {
	if len(r) == 0 || string(r) == "null" {
		return 0
	}
	return len(r)
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// observe folds one event into the run's state and the totals.
func (c *collector) observe(ra *runAcc, data string, at time.Time) {
	var r row
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		r = row{Type: -1}
	}
	n := len(data)
	ra.rows++
	ra.bytes += int64(n)
	ta := c.typ(r.Type)
	ta.rows++
	ta.bytes += int64(n)
	ta.sizes = append(ta.sizes, int32(n))
	pa := c.prov(ra.provider)
	pa.rows++
	pa.bytes += int64(n)

	isText := r.Type == int(taskrunner.EventTypeText)
	isThink := r.Type == int(taskrunner.EventTypeThinking)
	if isText || isThink {
		text := nonEmpty(r.Text)
		env := n - text
		pa.chunk = append(pa.chunk, int32(text))
		c.chunkRows++
		c.chunkBytes += int64(n)
		ra.bound.add(r.Type, text, env, false, &c.bound)
		gap := !ra.lastAt.IsZero() && !at.IsZero() && at.Sub(ra.lastAt) >= ChunkIdle
		ra.idle.add(r.Type, text, env, gap, &c.idle)
		ra.lastAt = at
		if isText {
			if ra.runText == 0 {
				ra.runEnv = env
			}
			ra.runText += text
			ra.thinkOpen = false
		} else {
			c.closeText(ra)
			c.thinkBytes += int64(n)
			if !ra.thinkOpen {
				ra.thinkOpen = true
				c.otherRows++
				c.otherBytes += ThinkingRowBytes
			}
		}
		return
	}
	// A non-chunk event is a boundary for every open buffer.
	ra.bound.flush(&c.bound)
	ra.idle.flush(&c.idle)
	ra.lastAt = time.Time{}
	c.closeText(ra)
	ra.thinkOpen = false

	if r.Type == int(taskrunner.EventTypeToolCall) {
		c.toolEvent(ra, pa, r)
		return
	}
	c.otherRows++
	c.otherBytes += int64(n)
}

// closeText ends the open text run: one skeleton answer row, capped.
func (c *collector) closeText(ra *runAcc) {
	if ra.runText == 0 {
		return
	}
	c.answerRuns = append(c.answerRuns, int32(ra.runText))
	c.answerRows++
	c.answerBytes += int64(ra.runEnv + min(ra.runText, AnswerRowCap))
	ra.runText = 0
}

func (c *collector) closeRun(ra *runAcc) {
	ra.bound.flush(&c.bound)
	ra.idle.flush(&c.idle)
	c.closeText(ra)
}

// toolEvent records the payload sizes of one tool_call event. A distinct call
// id costs one skeleton tool row; each payload adds its summary, and one over
// the inline cap adds a descriptor and an excerpt row.
func (c *collector) toolEvent(ra *runAcc, pa *provAcc, r row) {
	name := label(r.ToolName)
	if r.ToolName == "" {
		name = "unknown"
	}
	ta := c.tool(name)
	if _, seen := ra.calls[hash(r.ToolCallID)]; !seen && r.ToolCallID != "" {
		ra.calls[hash(r.ToolCallID)] = struct{}{}
		c.toolCalls++
		ta.calls++
		c.toolRows++
		c.toolBytes += ToolRowFixed
	}
	payload := func(size int, summaryCap int, dst *[]int32, pdst *[]int32) {
		if size == 0 {
			return
		}
		*dst = append(*dst, int32(size))
		*pdst = append(*pdst, int32(size))
		c.payloads = append(c.payloads, int32(size))
		ta.bytes += int64(size)
		c.toolBytes += int64(min(size, summaryCap))
		if size > DefaultInline {
			c.toolBytes += DescriptorBytes
			c.excerptRows++
			c.excerptB += int64(min(size, DefaultWhole))
		}
	}
	payload(nonEmpty(r.ToolInput), ArgsSummaryCap, &ta.input, &pa.input)
	payload(nonEmpty(r.ToolResult), ResultSummaryCap, &ta.result, &pa.result)
}

// ADR-0053 starting thresholds, used by the whole-database estimate.
const (
	DefaultInline = 512
	DefaultWhole  = 8 << 10
)

func (c *collector) finishRun(ra *runAcc, info runInfo, now time.Time) {
	c.rowsPerRun = append(c.rowsPerRun, int32(ra.rows))
	c.bytesRun = append(c.bytesRun, clamp(ra.bytes))
	pa := c.prov(ra.provider)
	pa.runs++
	pa.rowsPerRun = append(pa.rowsPerRun, int32(ra.rows))
	pa.bytesPerRun = append(pa.bytesPerRun, clamp(ra.bytes))
	key := "unknown"
	if !info.started.IsZero() {
		switch age := now.Sub(info.started); {
		case age < 7*24*time.Hour:
			key = "0-7d"
		case age < 30*24*time.Hour:
			key = "7-30d"
		default:
			key = "30d+"
		}
	}
	b := c.age[key]
	b.Rows += ra.rows
	b.Bytes += ra.bytes
	c.age[key] = b
}

func clamp(n int64) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

func dist(sizes []int32) Dist {
	d := Dist{N: len(sizes)}
	if len(sizes) == 0 {
		return d
	}
	s := append([]int32(nil), sizes...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	pick := func(p float64) int {
		i := int(math.Ceil(p*float64(len(s)))) - 1
		return int(s[max(i, 0)])
	}
	for _, v := range s {
		d.Sum += int64(v)
	}
	d.P50, d.P90, d.P99, d.Max = pick(0.50), pick(0.90), pick(0.99), int(s[len(s)-1])
	return d
}

func (c *collector) report(ctx context.Context, db *sql.DB, path string, now time.Time, nrows, total int64, nruns int) (*Report, error) {
	rep := &Report{GeneratedAt: now.UTC(), Runs: nruns, Rows: nrows, EventBytes: total,
		RowsPerRun: dist(c.rowsPerRun), BytesPerRun: dist(c.bytesRun), ByAge: c.age,
		ToolCalls: c.toolCalls, Payload: dist(c.payloads)}
	rep.DB = dbPages(ctx, db, path, nrows, total)
	ov := rep.DB.RowOverheadBytes

	for t, a := range c.types {
		rep.ByType = append(rep.ByType, TypeStats{Type: typeName(t), Bucket: Bucket{a.rows, a.bytes}, Size: dist(a.sizes)})
	}
	sort.Slice(rep.ByType, func(i, j int) bool { return rep.ByType[i].Bytes > rep.ByType[j].Bytes })
	for n, a := range c.providers {
		rep.ByProvider = append(rep.ByProvider, ProviderStats{Provider: n, Runs: a.runs, Rows: a.rows, Bytes: a.bytes,
			RowsPerRun: dist(a.rowsPerRun), BytesPerRun: dist(a.bytesPerRun),
			TextChunk: dist(a.chunk), ToolInput: dist(a.input), ToolResult: dist(a.result)})
	}
	sort.Slice(rep.ByProvider, func(i, j int) bool { return rep.ByProvider[i].Bytes > rep.ByProvider[j].Bytes })
	rep.ByTool = c.toolTable()

	rep.Coalescing = Coalescing{
		ChunkRowsNow: c.chunkRows, ChunkBytesNow: c.chunkBytes,
		RowsBoundaryAnd4K: c.bound[0], BytesBoundaryAnd4K: c.bound[1],
		RowsWithIdle: c.idle[0], BytesWithIdle: c.idle[1],
		SavedBytesWithIdle: (c.chunkBytes - c.idle[1]) + (c.chunkRows-c.idle[0])*ov,
		AnswerRun:          dist(c.answerRuns),
	}
	rep.Thresholds = c.thresholds()

	cur := total + nrows*ov
	coal := total - c.chunkBytes + c.idle[1] + (nrows-c.chunkRows+c.idle[0])*ov
	skelRows := c.otherRows + c.answerRows + c.toolRows
	skelBytes := c.otherBytes + c.answerBytes + c.toolBytes
	skelOnly := skelBytes + skelRows*ov
	rep.Estimate = Estimate{
		CurrentBytes: cur, AfterCoalescingBytes: coal,
		SkeletonOnlyBytes:         skelOnly,
		SkeletonWithExcerptsBytes: skelOnly + c.excerptB + c.excerptRows*ov,
		ThinkingBytesNow:          c.thinkBytes,
		Assumptions: fmt.Sprintf("row overhead %d B (%s); skeleton tool row %d B + args summary <=%d B + result summary <=%d B + %d B descriptor per payload >%d B; "+
			"thinking run -> one %d B row; answer row <=%d B; excerpt tier whole up to %d B, else %d B (head+tail); non-chunk, non-tool rows kept as they are",
			ov, rep.DB.RowOverheadSource, ToolRowFixed, ArgsSummaryCap, ResultSummaryCap, DescriptorBytes, DefaultInline,
			ThinkingRowBytes, AnswerRowCap, DefaultWhole, DefaultWhole),
	}
	return rep, nil
}

const otherTools = "(other)"

// toolTable keeps the 12 tools with the most payload bytes and folds the rest.
func (c *collector) toolTable() []ToolStats {
	type kv struct {
		name string
		a    *toolAcc
	}
	var all []kv
	for n, a := range c.tools {
		all = append(all, kv{n, a})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].a.bytes > all[j].a.bytes })
	var out []ToolStats
	rest := &toolAcc{}
	for i, e := range all {
		if i < 12 {
			out = append(out, ToolStats{Tool: e.name, Calls: e.a.calls, Input: dist(e.a.input), Result: dist(e.a.result)})
			continue
		}
		rest.calls += e.a.calls
		rest.input = append(rest.input, e.a.input...)
		rest.result = append(rest.result, e.a.result...)
	}
	if len(all) > 12 {
		out = append(out, ToolStats{Tool: otherTools, Calls: rest.calls, Input: dist(rest.input), Result: dist(rest.result)})
	}
	return out
}

// thresholds evaluates the payload bytes kept for each (inline, whole) pair.
func (c *collector) thresholds() []Threshold {
	var total int64
	for _, s := range c.payloads {
		total += int64(s)
	}
	var out []Threshold
	for _, inl := range inlineGrid {
		for _, whole := range wholeGrid {
			t := Threshold{InlineCap: inl, WholeCap: whole}
			var kept int
			for _, s32 := range c.payloads {
				s := int(s32)
				switch {
				case s <= inl:
					t.InlineBytes += int64(s)
					kept++
				case s <= whole:
					t.ExcerptTierBytes += int64(s)
					t.DescriptorBytes += DescriptorBytes
					kept++
				default:
					t.ExcerptTierBytes += int64(whole)
					t.DescriptorBytes += DescriptorBytes
				}
			}
			if n := len(c.payloads); n > 0 {
				t.PayloadsWhole = round1(100 * float64(kept) / float64(n))
			}
			t.SkeletonOnlyBytes = t.InlineBytes + t.DescriptorBytes
			t.WithExcerptBytes = t.SkeletonOnlyBytes + t.ExcerptTierBytes
			if total > 0 {
				t.SavedPct = round1(100 * float64(total-t.WithExcerptBytes) / float64(total))
			}
			out = append(out, t)
		}
	}
	return out
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// dbPages reads the file's page accounting. The per-row overhead (table and
// index bytes beyond event_data, page slack included) is calibrated from
// dbstat; a database without it falls back to an assumed value.
func dbPages(ctx context.Context, db *sql.DB, path string, nrows, eventBytes int64) DB {
	out := DB{RowOverheadBytes: 150, RowOverheadSource: "assumed"}
	if st, err := os.Stat(path); err == nil {
		out.FileBytes = st.Size()
	}
	var pageSize, free int64
	_ = db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize)
	_ = db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&free)
	out.FreelistBytes = free * pageSize
	rows, err := db.QueryContext(ctx, `SELECT s.name, COALESCE(m.tbl_name, s.name), SUM(s.pgsize) FROM dbstat s LEFT JOIN sqlite_master m ON m.name = s.name GROUP BY s.name`)
	if err != nil {
		return out
	}
	defer rows.Close()
	var table, index int64
	for rows.Next() {
		var name, tbl string
		var size int64
		if rows.Scan(&name, &tbl, &size) != nil {
			return out
		}
		switch {
		case name == "run_events":
			table += size
		case tbl == "run_events":
			index += size
		default:
			out.OtherBytes += size
		}
	}
	out.RunEventsTableB, out.RunEventsIndexB = table, index
	if nrows > 0 && table+index > eventBytes {
		out.RowOverheadBytes = (table + index - eventBytes) / nrows
		out.RowOverheadSource = "measured from dbstat"
	}
	return out
}
