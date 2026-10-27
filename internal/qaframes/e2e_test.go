package qaframes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spacingmind/smind/internal/validation"
)

// A real browser against a real isolated daemon. It needs node, a playwright
// module, a Chromium and a built web UI (internal/server/dist/index.html);
// without them it SKIPS with the reason, it does not pass silently.
func TestFramesRun_E2E_RealBrowser(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	env := OSEnv(root)
	if _, err := DetectBrowser(env, "", ""); err != nil {
		t.Skipf("no real browser here (%v): the E2E did not run", err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "server", "dist", "index.html")); err != nil {
		t.Skip("the web UI is not built (task build:web): the E2E did not run")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "smind")
	build := exec.Command("go", "build", "-o", bin, "./cmd/smind")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	evRoot := t.TempDir()
	l := &RealLauncher{Bin: bin, RepoRoot: root, Log: os.Stderr}
	tmpRoot := t.TempDir()
	rep, err := Run(context.Background(), Options{
		Frames:   []validation.Frame{{ID: "F1", Surface: "web /", State: "shell at rest", Viewport: []int{1280, 390}, Theme: []string{"light", "dark"}}},
		RepoRoot: evRoot, Env: env, Launcher: l, TempRoot: tmpRoot,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Status != StatusCaptured || len(rep.Frames) != 1 || len(rep.Frames[0].Variants) != 4 {
		t.Fatalf("report = %+v", rep)
	}
	for _, v := range rep.Frames[0].Variants {
		if v.Error != "" || len(v.Files) < 2 || len(v.Sheets) == 0 {
			t.Errorf("variant %s %v: %+v", v.Theme, v.Viewport, v)
		}
		for _, s := range v.Sheets {
			if len(s.Cells) > MaxFramesPerSheet {
				t.Errorf("sheet over %d", MaxFramesPerSheet)
			}
			if st, err := os.Stat(filepath.Join(evRoot, s.File)); err != nil || st.Size() < 1000 {
				t.Errorf("sheet %s not written: %v", s.File, err)
			}
		}
	}
	if left, _ := filepath.Glob(filepath.Join(tmpRoot, "smind-qa-frames-*")); len(left) != 0 {
		t.Errorf("temp home left behind: %v", left)
	}
}
