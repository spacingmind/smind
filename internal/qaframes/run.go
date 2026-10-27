package qaframes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/validation"
)

// DefaultEvidenceRel is where, inside the validator's worktree, the report and
// frames go: under the evidence subdir the validator already cites.
var DefaultEvidenceRel = filepath.ToSlash(filepath.Join(validation.EvidenceSubdir, "qa-frames"))

// Options configure one Run.
type Options struct {
	Frames        []validation.Frame
	Scenarios     map[string][]Step // by frame id; absent = default steps
	RepoRoot      string            // worktree root; evidence paths are relative to it
	EvidenceRel   string            // default DefaultEvidenceRel
	ChangedFiles  []string          // repo-relative, for the diff mapping
	AllowSend     bool
	Model         string
	Env           Env
	Chrome        string // overrides
	PlaywrightDir string
	Launcher      Launcher
	TempRoot      string // where the isolated home is made; default os.TempDir()
}

// DriverShot is one unit of browser work.
type DriverShot struct {
	Key      string `json:"key"`
	Dir      string `json:"dir"`
	Route    string `json:"route"`
	Viewport []int  `json:"viewport"`
	Theme    string `json:"theme"`
	Steps    []Step `json:"steps"`
}

// DriverFile is one image the driver wrote.
type DriverFile struct {
	File string `json:"file"`
	Name string `json:"name"`
	TMs  int    `json:"tMs"`
}

// DriverResult is the driver's answer for one shot.
type DriverResult struct {
	Key   string       `json:"key"`
	Files []DriverFile `json:"files"`
	Steps []string     `json:"steps"`
	Error string       `json:"error"`
}

// Launcher is everything Run starts or contacts; tests substitute fakes.
type Launcher interface {
	// Prepare builds or locates the binary to serve (and the fake agent when asked).
	Prepare(ctx context.Context, sup *Supervisor, env Env, tmp string) (bin string, err error)
	// StartDaemon starts the isolated daemon and returns its base URL once ready.
	StartDaemon(ctx context.Context, sup *Supervisor, bin, home string, port int) (base string, err error)
	// RunDriver shoots every shot in one browser and returns results in order.
	RunDriver(ctx context.Context, sup *Supervisor, b Browser, base string, shots []DriverShot, allowSend bool, model string) ([]DriverResult, error)
}

// ErrToolchain is returned (with the report written) when the UI bundle or the
// binary cannot be had: the reason is no_toolchain, not a pass.
var ErrToolchain = errors.New("no toolchain")

// Run executes the whole flow and always writes the report (when it can
// create the evidence dir). A blocked run returns the report together with
// ErrNoBrowser / ErrToolchain; any process it started is dead and the
// temporary home is gone when it returns.
func Run(ctx context.Context, o Options) (*Report, error) {
	if o.EvidenceRel == "" {
		o.EvidenceRel = DefaultEvidenceRel
	}
	if !validation.SafeRelPath(o.EvidenceRel) {
		return nil, fmt.Errorf("evidence dir %q must be a relative path inside the worktree", o.EvidenceRel)
	}
	// Guard first: nothing is started when a send is not allowed.
	if err := CheckSends(o.Scenarios, o.AllowSend, o.Model); err != nil {
		return nil, err
	}
	evDir := filepath.Join(o.RepoRoot, filepath.FromSlash(o.EvidenceRel))
	rep := newReport(o.Frames, o.EvidenceRel, MapDiff(o.ChangedFiles))
	finish := func(err error) (*Report, error) {
		if werr := rep.Write(evDir); werr != nil && err == nil {
			err = werr
		}
		return rep, err
	}

	b, err := DetectBrowser(o.Env, o.Chrome, o.PlaywrightDir)
	if err != nil {
		var nb *NoBrowserError
		if errors.As(err, &nb) {
			rep.blockAll(ReasonNoBrowser, nb.Error())
			rep.Notes = append(rep.Notes, "reported as blocked: no_browser; the frame items stay blocked exactly as without the harness")
			return finish(err)
		}
		return finish(err)
	}
	rep.Browser = b.String()

	tmp, err := os.MkdirTemp(o.TempRoot, "smind-qa-frames-")
	if err != nil {
		return finish(err)
	}
	sup := NewSupervisor()
	defer func() { sup.KillAll(); _ = os.RemoveAll(tmp) }()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return finish(err)
	}
	port, err := freePort()
	if err != nil {
		return finish(err)
	}
	callerHome, _ := os.UserHomeDir()
	if err := CheckIsolation(home, port, callerHome, o.Env.Getenv("SMIND_HOME")); err != nil {
		return finish(err)
	}

	bin, err := o.Launcher.Prepare(ctx, sup, o.Env, tmp)
	if err != nil {
		rep.blockAll(ReasonNoToolchain, err.Error())
		return finish(fmt.Errorf("%w: %v", ErrToolchain, err))
	}
	base, err := o.Launcher.StartDaemon(ctx, sup, bin, home, port)
	if err != nil {
		rep.blockAll(ReasonNoToolchain, err.Error())
		return finish(fmt.Errorf("%w: %v", ErrToolchain, err))
	}
	rep.Daemon = base

	var shots []DriverShot
	type vkey struct {
		frame, theme string
		w            int
	}
	for i := range rep.Frames {
		fr := &rep.Frames[i]
		if fr.Status == StatusBlocked {
			continue
		}
		cf := o.Frames[i]
		vps, themes := cf.Viewport, cf.Theme
		if len(vps) == 0 {
			vps = []int{1280, 390}
		}
		if len(themes) == 0 {
			themes = []string{"light", "dark"}
		}
		for _, w := range vps {
			for _, th := range themes {
				if th != "light" && th != "dark" {
					fr.Notes = append(fr.Notes, fmt.Sprintf("theme %q is not light or dark: skipped (reported, not shot)", th))
					continue
				}
				h := 800
				if w < 600 {
					h = 844
				}
				steps := o.Scenarios[fr.ID]
				if len(steps) == 0 {
					steps = defaultSteps()
				}
				shots = append(shots, DriverShot{
					Key: fmt.Sprintf("%s|%d|%s", fr.ID, w, th), Dir: filepath.Join(evDir, fr.ID, fmt.Sprintf("%s-%d", th, w)),
					Route: fr.Route, Viewport: []int{w, h}, Theme: th, Steps: steps,
				})
			}
		}
	}
	results, err := o.Launcher.RunDriver(ctx, sup, b, base, shots, o.AllowSend, o.Model)
	if err != nil {
		for i := range rep.Frames {
			if rep.Frames[i].Status == StatusCaptured {
				rep.Frames[i].Status, rep.Frames[i].BlockedReason = StatusBlocked, ReasonRunFailed
				rep.Frames[i].Notes = append(rep.Frames[i].Notes, err.Error())
			}
		}
		rep.Status, rep.BlockedReason = StatusBlocked, ReasonRunFailed
		return finish(err)
	}
	byKey := map[string]DriverResult{}
	for _, r := range results {
		byKey[r.Key] = r
	}
	for _, sh := range shots {
		id, th := splitKey(sh.Key)
		fr := frameByID(rep, id)
		res, ok := byKey[sh.Key]
		v := Variant{Viewport: sh.Viewport, Theme: th, Steps: res.Steps, Files: []string{}, Sheets: []SheetInfo{}}
		if !ok {
			v.Error = "the driver returned no result for this shot"
		}
		v.Error = firstNonEmpty(v.Error, res.Error)
		var cells []Cell
		for _, f := range res.Files {
			rel, _ := filepath.Rel(o.RepoRoot, f.File)
			v.Files = append(v.Files, filepath.ToSlash(rel))
			cells = append(cells, Cell{File: f.File, Label: cellLabel(f), TMs: f.TMs})
		}
		sheets, serr := WriteSheets(sh.Dir, "contact", cells)
		if serr != nil {
			v.Error = firstNonEmpty(v.Error, "contact sheet: "+serr.Error())
		}
		for _, s := range sheets {
			full := filepath.Join(sh.Dir, s.File)
			rel, _ := filepath.Rel(o.RepoRoot, full)
			s.File = filepath.ToSlash(rel)
			for k := range s.Cells {
				r, _ := filepath.Rel(o.RepoRoot, s.Cells[k].File)
				s.Cells[k].File = filepath.ToSlash(r)
			}
			v.Sheets = append(v.Sheets, s)
		}
		if v.Error != "" || len(v.Files) == 0 {
			fr.Status, fr.BlockedReason = StatusBlocked, ReasonRunFailed
			fr.Notes = append(fr.Notes, fmt.Sprintf("%s %d: %s", th, sh.Viewport[0], firstNonEmpty(v.Error, "no images captured")))
		}
		fr.Variants = append(fr.Variants, v)
	}
	for _, fr := range rep.Frames {
		if fr.Status == StatusBlocked && rep.Status == StatusCaptured {
			rep.Status, rep.BlockedReason = StatusBlocked, firstNonEmpty(fr.BlockedReason)
		}
	}
	return finish(nil)
}

func defaultSteps() []Step {
	return []Step{
		{Do: "goto"}, {Do: "wait", Ms: 1200}, {Do: "shot", Name: "idle"},
		{Do: "burst", Name: "reload", Frames: 10, GapMs: 80, Then: &Step{Do: "reload"}},
	}
}

func cellLabel(f DriverFile) string {
	l := f.Name
	if f.TMs > 0 || strings.Contains(f.Name, "-t") {
		l = fmt.Sprintf("%s +%dMS", f.Name, f.TMs)
	}
	if len(l) > 38 {
		l = l[:38]
	}
	return l
}

func splitKey(k string) (id, theme string) {
	p := strings.Split(k, "|")
	if len(p) != 3 {
		return k, ""
	}
	return p[0], p[2]
}

func frameByID(r *Report, id string) *FrameResult {
	for i := range r.Frames {
		if r.Frames[i].ID == id {
			return &r.Frames[i]
		}
	}
	return &FrameResult{}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// freePort asks the kernel for a free loopback port. The window between
// closing the probe and the daemon binding is small; a collision fails the
// daemon start, which is reported, never silently shared.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
