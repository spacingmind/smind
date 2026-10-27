package qaframes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spacingmind/smind/internal/validation"
)

// Frame statuses. "captured" means the screens were shot; LOOKING at them and
// writing the observation is the validator's job, so it is never "pass".
const (
	StatusCaptured = "captured"
	StatusBlocked  = "blocked"
)

// Blocked reasons, a subset of the verdict's blockedReason values.
const (
	ReasonNoBrowser   = validation.ReasonNoBrowser
	ReasonNoToolchain = "no_toolchain"
	ReasonUnmapped    = "unmapped_surface"
	ReasonRunFailed   = "run_failed"
)

// Variant is one viewport x theme shoot of one frame.
type Variant struct {
	Viewport []int       `json:"viewport"`
	Theme    string      `json:"theme"`
	Steps    []string    `json:"steps"`
	Files    []string    `json:"files"`  // every frame image, repo-relative
	Sheets   []SheetInfo `json:"sheets"` // contact sheets, <= 12 cells each
	Error    string      `json:"error,omitempty"`
}

// FrameResult is the outcome for one contract frame item.
type FrameResult struct {
	ID            string    `json:"id"`
	Surface       string    `json:"surface"`
	State         string    `json:"state"`
	Route         string    `json:"route,omitempty"`
	Status        string    `json:"status"`
	BlockedReason string    `json:"blockedReason,omitempty"`
	Notes         []string  `json:"notes,omitempty"`
	Variants      []Variant `json:"variants"`
}

// Report is the evidence report: report.json and report.md in the evidence dir.
type Report struct {
	V             int           `json:"v"`
	Status        string        `json:"status"` // captured | blocked
	BlockedReason string        `json:"blockedReason,omitempty"`
	Platform      string        `json:"platform"`
	Browser       string        `json:"browser,omitempty"`
	Daemon        string        `json:"daemon,omitempty"` // isolated address, for the record
	Mapping       Mapping       `json:"mapping"`
	Frames        []FrameResult `json:"frames"`
	Notes         []string      `json:"notes,omitempty"`
	EvidenceDir   string        `json:"evidenceDir"`
}

// newReport lists every contract frame, so none can go missing from the report.
func newReport(frames []validation.Frame, evidenceDir string, m Mapping) *Report {
	r := &Report{V: 1, Status: StatusCaptured, Platform: runtime.GOOS + "/" + runtime.GOARCH, Mapping: m, EvidenceDir: evidenceDir}
	for _, f := range frames {
		fr := FrameResult{ID: f.ID, Surface: f.Surface, State: f.State, Status: StatusCaptured, Variants: []Variant{}}
		if route, ok := SurfaceRoute(f.Surface); ok {
			fr.Route = route
		} else {
			fr.Status, fr.BlockedReason = StatusBlocked, ReasonUnmapped
			fr.Notes = append(fr.Notes, fmt.Sprintf("surface %q is not a web route (expected \"web /path\"); shoot it by hand or add a scenario", f.Surface))
		}
		r.Frames = append(r.Frames, fr)
	}
	return r
}

// blockAll marks every frame blocked with reason and returns the report.
func (r *Report) blockAll(reason, note string) {
	r.Status, r.BlockedReason = StatusBlocked, reason
	for i := range r.Frames {
		r.Frames[i].Status, r.Frames[i].BlockedReason = StatusBlocked, reason
		r.Frames[i].Notes = append(r.Frames[i].Notes, note)
	}
}

// Write writes report.json and report.md under dir (created).
func (r *Report) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(r.Markdown()), 0o644)
}

// Markdown renders the report for a human or a model to read.
func (r *Report) Markdown() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("# QA frames report")
	w("")
	w("Status: **%s**%s. Platform %s.", r.Status, ifNotEmpty(" ("+r.BlockedReason+")", r.BlockedReason), r.Platform)
	w("A frame is `captured` when its screens were shot. Looking at them, and the observation, is the validator's job; this report is not a pass.")
	if r.Browser != "" {
		w("Browser: `%s`", r.Browser)
	}
	for _, n := range r.Notes {
		w("- %s", n)
	}
	w("")
	w("## Diff to screens")
	for _, t := range r.Mapping.Targets {
		w("- **%s** (`%s`): %s; from %s", t.Name, t.Route, t.Hint, strings.Join(t.Files, ", "))
	}
	for _, f := range r.Mapping.Unmapped {
		w("- UNMAPPED: `%s` (no screen mapped: pick one and shoot it)", f)
	}
	for _, f := range r.Mapping.Ignored {
		w("- ignored: `%s` (%s)", f.File, f.Reason)
	}
	w("")
	w("## Frames")
	for _, f := range r.Frames {
		w("")
		w("### %s: %s, state %q: %s%s", f.ID, f.Surface, f.State, f.Status, ifNotEmpty(" ("+f.BlockedReason+")", f.BlockedReason))
		for _, n := range f.Notes {
			w("- note: %s", n)
		}
		for _, v := range f.Variants {
			w("- %s %dx%d: %d images%s", v.Theme, v.Viewport[0], v.Viewport[1], len(v.Files), ifNotEmpty(", ERROR: "+v.Error, v.Error))
			w("  - steps: %s", strings.Join(v.Steps, " > "))
			for _, s := range v.Sheets {
				w("  - sheet `%s` (%d frames)", s.File, len(s.Cells))
			}
		}
	}
	return b.String()
}

func ifNotEmpty(s, cond string) string {
	if cond == "" {
		return ""
	}
	return s
}
