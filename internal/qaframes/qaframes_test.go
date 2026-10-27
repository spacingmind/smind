package qaframes

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/validation"
)

func TestFramesPlan_DiffToScreens_MappedAndUnmapped(t *testing.T) {
	m := MapDiff([]string{
		"web/packages/ui/src/components/inbox/inbox-screen.tsx",
		"web/packages/ui/src/components/inbox/inbox-badge.tsx",
		"web/packages/ui/src/components/brand-new-widget/widget.tsx",
		"web/packages/ui/src/components/inbox/inbox-screen.test.tsx",
		"internal/validation/service.go",
	})
	if len(m.Targets) != 1 || m.Targets[0].Name != "inbox" || len(m.Targets[0].Files) != 2 {
		t.Fatalf("targets = %+v", m.Targets)
	}
	if len(m.Unmapped) != 1 || !strings.Contains(m.Unmapped[0], "brand-new-widget") {
		t.Fatalf("an unknown UI file must be reported unmapped: %+v", m.Unmapped)
	}
	reasons := map[string]string{}
	for _, i := range m.Ignored {
		reasons[i.File] = i.Reason
	}
	if reasons["web/packages/ui/src/components/inbox/inbox-screen.test.tsx"] != "test file" || reasons["internal/validation/service.go"] == "" {
		t.Fatalf("ignored = %+v", m.Ignored)
	}
	if n := len(m.Targets[0].Files) + len(m.Unmapped) + len(m.Ignored); n != 5 {
		t.Fatalf("every file is accounted for exactly once, got %d", n)
	}
	for in, want := range map[string]string{"web /inbox": "#/inbox", "web #/workspace/1/task/1/chat/1": "#/workspace/1/task/1/chat/1", "web /": "#/"} {
		if got, ok := SurfaceRoute(in); !ok || got != want {
			t.Errorf("SurfaceRoute(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := SurfaceRoute("mobile home"); ok {
		t.Errorf("a non-web surface must not map")
	}
}

// ---- fakes ----

type fakeLauncher struct {
	prepared, daemons, drivers int
	home                       string
	port                       int
	sup                        *Supervisor
	onDaemon                   func(sup *Supervisor, home string) error
	driver                     func(ctx context.Context, sup *Supervisor, shots []DriverShot) ([]DriverResult, error)
}

func (f *fakeLauncher) Prepare(ctx context.Context, sup *Supervisor, env Env, tmp string) (string, error) {
	f.prepared++
	return "fake-bin", nil
}
func (f *fakeLauncher) StartDaemon(ctx context.Context, sup *Supervisor, bin, home string, port int) (string, error) {
	f.daemons++
	f.home, f.port, f.sup = home, port, sup
	if f.onDaemon != nil {
		if err := f.onDaemon(sup, home); err != nil {
			return "", err
		}
	}
	return "http://127.0.0.1:1", nil
}
func (f *fakeLauncher) RunDriver(ctx context.Context, sup *Supervisor, b Browser, base string, shots []DriverShot, allowSend bool, model string) ([]DriverResult, error) {
	f.drivers++
	if f.driver != nil {
		return f.driver(ctx, sup, shots)
	}
	return writeFakeShots(shots)
}

func writeFakeShots(shots []DriverShot) ([]DriverResult, error) {
	var out []DriverResult
	for _, s := range shots {
		if err := os.MkdirAll(s.Dir, 0o755); err != nil {
			return nil, err
		}
		r := DriverResult{Key: s.Key, Steps: []string{"goto", "shot"}}
		for i := 0; i < 3; i++ {
			f := filepath.Join(s.Dir, "00"+string(rune('0'+i))+"-x.png")
			writePNG(f, 40, 30)
			r.Files = append(r.Files, DriverFile{File: f, Name: "x", TMs: i * 80})
		}
		out = append(out, r)
	}
	return out, nil
}

func writePNG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{0x20, 0x40, 0x60, 0xff})
		}
	}
	f, _ := os.Create(path)
	defer f.Close()
	_ = png.Encode(f, img)
}

type fakeFileInfo struct{ os.FileInfo }

func (fakeFileInfo) IsDir() bool { return false }

// browserEnv is an Env that has node, playwright and chrome; missing names pieces to remove.
func browserEnv(root string, missing ...string) Env {
	gone := map[string]bool{}
	for _, m := range missing {
		gone[m] = true
	}
	return Env{
		RepoRoot: root, Home: "/nonexistent-home", GOOS: "linux",
		Getenv: func(k string) string { return "" },
		LookPath: func(n string) (string, error) {
			if n == "node" && !gone["node"] {
				return "/usr/bin/node", nil
			}
			return "", errors.New("not found")
		},
		Glob: func(string) ([]string, error) { return nil, nil },
		Stat: func(p string) (os.FileInfo, error) {
			p = filepath.ToSlash(p) // detect.go joins with the host separator
			switch {
			case strings.HasSuffix(p, "playwright-core/package.json") && !gone["playwright"]:
				return fakeFileInfo{}, nil
			case strings.HasSuffix(p, "fake-chrome") && !gone["chrome"]:
				return fakeFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}
}

func testFrames() []validation.Frame {
	return []validation.Frame{
		{ID: "F1", Surface: "web /inbox", State: "one item", Viewport: []int{1280, 390}, Theme: []string{"light", "dark"}},
		{ID: "F2", Surface: "web /", State: "idle"},
		{ID: "F3", Surface: "desktop titlebar", State: "native"},
	}
}

func baseOpts(t *testing.T, l Launcher, env Env) Options {
	root := t.TempDir()
	env.RepoRoot = root
	return Options{Frames: testFrames(), RepoRoot: root, Env: env, Chrome: "/x/fake-chrome", Launcher: l, TempRoot: t.TempDir()}
}

func TestFramesRun_NoBrowser_TypedBlocked(t *testing.T) {
	for _, missing := range []string{"node", "playwright", "chrome"} {
		l := &fakeLauncher{}
		o := baseOpts(t, l, browserEnv("", missing))
		rep, err := Run(context.Background(), o)
		if !errors.Is(err, ErrNoBrowser) {
			t.Fatalf("%s missing: err = %v, want ErrNoBrowser", missing, err)
		}
		if rep.Status != StatusBlocked || rep.BlockedReason != ReasonNoBrowser {
			t.Fatalf("%s missing: report = %s/%s", missing, rep.Status, rep.BlockedReason)
		}
		for _, f := range rep.Frames {
			if f.Status != StatusBlocked || f.BlockedReason != ReasonNoBrowser {
				t.Errorf("%s missing: frame %s = %s/%s, a missing browser is never a pass", missing, f.ID, f.Status, f.BlockedReason)
			}
		}
		if l.prepared+l.daemons+l.drivers != 0 {
			t.Errorf("%s missing: nothing may be started without a browser", missing)
		}
		if _, err := os.Stat(filepath.Join(o.RepoRoot, DefaultEvidenceRel, "report.json")); err != nil {
			t.Errorf("%s missing: the blocked report must still be written: %v", missing, err)
		}
	}
	// and with everything present it is not blocked
	l := &fakeLauncher{}
	if _, err := Run(context.Background(), baseOpts(t, l, browserEnv(""))); err != nil {
		t.Fatalf("with a browser: %v", err)
	}
}

func TestFramesRun_IsolatedHome_NeverTouchesDefaultHomeOrPorts(t *testing.T) {
	// Hermetic: the "default home" this test compares against is a temp HOME,
	// so no path of the Human's real home is ever resolved, even by a mutant.
	fakeHome := t.TempDir()
	smindHome := filepath.Join(t.TempDir(), "unused-smind-home")
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)
	t.Setenv("SMIND_HOME", smindHome)
	benv := browserEnv("")
	benv.Getenv = func(k string) string {
		if k == "SMIND_HOME" {
			return smindHome
		}
		return ""
	}
	o := baseOpts(t, nil, benv)
	var got struct {
		home string
		port int
	}
	var existed bool
	l := &fakeLauncher{onDaemon: func(sup *Supervisor, home string) error {
		st, err := os.Stat(home)
		existed = err == nil && st.IsDir()
		return nil
	}}
	o.Launcher = l
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	got.home, got.port = l.home, l.port
	real, _ := os.UserHomeDir() // == fakeHome
	if real != fakeHome {
		t.Fatalf("test is not hermetic: HOME resolves to %q", real)
	}
	if !existed || !strings.HasPrefix(got.home, o.TempRoot) {
		t.Errorf("home %q must be a fresh dir under the temp root %q", got.home, o.TempRoot)
	}
	if got.home == filepath.Join(real, ".smind") || got.home == real || got.home == smindHome {
		t.Errorf("the real home was used: %s", got.home)
	}
	for _, p := range ForbiddenPorts {
		if got.port == p {
			t.Errorf("forbidden port %d used", p)
		}
	}
	if _, err := os.Stat(got.home); !os.IsNotExist(err) {
		t.Errorf("the isolated home must be removed on exit")
	}
	// the guard itself
	d := t.TempDir()
	if CheckIsolation(d, 4648, "/h", "") == nil || CheckIsolation(d, 4870, "/h", "") == nil {
		t.Errorf("forbidden ports must be refused")
	}
	if CheckIsolation(d, 5000, "/h", d) == nil {
		t.Errorf("the caller's SMIND_HOME must be refused")
	}
	if CheckIsolation(d, 5000, filepath.Dir(d), "") != nil {
		t.Errorf("a fresh dir is fine")
	}
	os.MkdirAll(filepath.Join(d, ".smind"), 0o755)
	if CheckIsolation(filepath.Join(d, ".smind"), 5000, d, "") == nil {
		t.Errorf("~/.smind must be refused")
	}
	// the daemon's environment carries the isolated home only
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("SMIND_HOME", "/the/real/home")
	env := strings.Join((&RealLauncher{}).daemonEnv("/tmp/iso/home", "127.0.0.1:5000"), "\n")
	if strings.Contains(env, "secret") || strings.Contains(env, "/the/real/home") || !strings.Contains(env, "HOME=/tmp/iso/home") || !strings.Contains(env, "SMIND_HOME=/tmp/iso/home") {
		t.Errorf("daemon env = %s", env)
	}
}

func TestFramesRun_ScriptedSend_RefusesNonHaiku_OffByDefault(t *testing.T) {
	send := map[string][]Step{"F1": {{Do: "goto"}, {Do: "burst", Then: &Step{Do: "send", Text: "hi"}}}}
	cases := []struct {
		allow bool
		model string
		ok    bool
	}{
		{false, "", false},
		{false, HaikuModel, false},
		{true, "", false},
		{true, "claude-opus-5-5", false},
		{true, "claude-sonnet-5-5", false},
		{true, HaikuModel, true},
	}
	for _, c := range cases {
		l := &fakeLauncher{}
		o := baseOpts(t, l, browserEnv(""))
		o.Scenarios, o.AllowSend, o.Model = send, c.allow, c.model
		_, err := Run(context.Background(), o)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrSendRefused)) {
			t.Errorf("allow=%v model=%q: err = %v", c.allow, c.model, err)
		}
		if !c.ok && l.prepared+l.daemons+l.drivers != 0 {
			t.Errorf("allow=%v model=%q: a refused send must start nothing", c.allow, c.model)
		}
	}
	if err := CheckSends(map[string][]Step{"F1": {{Do: "shot"}}}, false, ""); err != nil {
		t.Errorf("a scenario without send needs no permission: %v", err)
	}
}

func TestFramesSheet_AtMost12PerSheet_Timestamped(t *testing.T) {
	dir := t.TempDir()
	var cells []Cell
	for i := 0; i < 30; i++ {
		f := filepath.Join(dir, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".png")
		writePNG(f, 80, 50)
		cells = append(cells, Cell{File: f, Label: "BURST +" + string(rune('0'+i%10)) + "MS", TMs: i * 50})
	}
	sheets, err := WriteSheets(dir, "contact", cells)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 3 || len(sheets[0].Cells) != 12 || len(sheets[1].Cells) != 12 || len(sheets[2].Cells) != 6 {
		t.Fatalf("sheets = %d, want 12/12/6", len(sheets))
	}
	for _, s := range sheets {
		if len(s.Cells) > MaxFramesPerSheet {
			t.Fatalf("sheet %s has %d cells", s.File, len(s.Cells))
		}
		for _, c := range s.Cells {
			if !strings.Contains(c.Label, "+") {
				t.Errorf("cell %q has no timestamp", c.Label)
			}
		}
		f, err := os.Open(filepath.Join(dir, s.File))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		white := 0
		for y := 0; y < sheetLabel; y++ { // the first label strip
			for x := 0; x < sheetCellW; x++ {
				if r, g, b, _ := img.At(sheetPad+x, sheetPad+y).RGBA(); r > 0xf000 && g > 0xf000 && b > 0xf000 {
					white++
				}
			}
		}
		if white < 20 {
			t.Errorf("sheet %s: the timestamp label was not drawn (%d label pixels)", s.File, white)
		}
	}
}

func TestFramesReport_MapsEveryContractFrame(t *testing.T) {
	l := &fakeLauncher{}
	o := baseOpts(t, l, browserEnv(""))
	o.ChangedFiles = []string{"web/packages/ui/src/components/inbox/inbox-screen.tsx", "web/packages/ui/src/components/mystery/m.tsx"}
	rep, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(o.RepoRoot, DefaultEvidenceRel, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Frames) != len(testFrames()) {
		t.Fatalf("report has %d frames, contract has %d", len(back.Frames), len(testFrames()))
	}
	by := map[string]FrameResult{}
	for _, f := range back.Frames {
		by[f.ID] = f
	}
	f1, f2, f3 := by["F1"], by["F2"], by["F3"]
	if f1.Status != StatusCaptured || len(f1.Variants) != 4 {
		t.Fatalf("F1 = %s with %d variants, want captured with 2 viewports x 2 themes", f1.Status, len(f1.Variants))
	}
	seen := map[string]bool{}
	for _, v := range f1.Variants {
		seen[v.Theme+"-"+string(rune('0'+v.Viewport[0]/100%10))] = true
		if len(v.Files) == 0 || len(v.Sheets) == 0 || len(v.Steps) == 0 {
			t.Errorf("variant %+v lacks files, sheets or steps", v)
		}
		for _, p := range v.Files {
			if filepath.IsAbs(p) || !strings.HasPrefix(p, validation.EvidenceSubdir+"/") {
				t.Errorf("evidence path %q must be relative, under %s", p, validation.EvidenceSubdir)
			}
			if _, err := os.Stat(filepath.Join(o.RepoRoot, p)); err != nil {
				t.Errorf("evidence file missing: %v", err)
			}
		}
	}
	if len(seen) != 4 {
		t.Errorf("variants = %v", seen)
	}
	if f2.Status != StatusCaptured || len(f2.Variants) != 4 {
		t.Errorf("F2 with no viewport/theme must default to 1280/390 x light/dark: %+v", f2)
	}
	if f3.Status != StatusBlocked || f3.BlockedReason != ReasonUnmapped || len(f3.Notes) == 0 {
		t.Errorf("F3 = %+v: an unmappable surface is blocked with a note, not dropped", f3)
	}
	if rep.Status != StatusBlocked || len(back.Mapping.Unmapped) != 1 || len(back.Mapping.Targets) != 1 {
		t.Errorf("report status %s, mapping %+v", rep.Status, back.Mapping)
	}
	if md, _ := os.ReadFile(filepath.Join(o.RepoRoot, DefaultEvidenceRel, "report.md")); !strings.Contains(string(md), "UNMAPPED") || !strings.Contains(string(md), "F3") {
		t.Errorf("report.md must list the unmapped file and every frame")
	}
}
