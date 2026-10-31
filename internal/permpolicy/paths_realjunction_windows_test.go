//go:build windows

package permpolicy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/realpath"
)

func TestResolveWindows_RealJunction(t *testing.T) {
	d := t.TempDir()
	target, wt := filepath.Join(d, "target"), filepath.Join(d, "wt")
	for _, p := range []string{target, wt} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(wt, "jn")
	if err := exec.Command("cmd", "/c", "mklink", "/J", link, target).Run(); err != nil {
		t.Skipf("mklink /J: %v", err)
	}
	got, err := ResolvePath(filepath.Join(link, "x"), "")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := realpath.Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(rt, "x"); got != want {
		t.Errorf("ResolvePath = %q; want %q", got, want)
	}
	if Within(got, []string{wt}) {
		t.Errorf("%q must not be within %q", got, wt)
	}
}
