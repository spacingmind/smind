package hygiene

import (
	"os/exec"
	"strings"
	"testing"
)

// The Tauri build rewrites desktop/src-tauri/gen/schemas on every build (capability schemas for editor autocompletion), so
// they are not source: untracked and ignored (docs/plans/active/desktop-generated-schemas.md). Tracking them again brings
// back the diffs that every Lead's build produced.
func TestGeneratedTauriSchemasAreIgnoredAndUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "--", "desktop/src-tauri/gen").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	if tracked := strings.TrimSpace(string(out)); tracked != "" {
		t.Errorf("generated Tauri files are tracked (run git rm -r --cached on them):\n%s", tracked)
	}
	for _, p := range []string{"desktop/src-tauri/gen/schemas/capabilities.json", "desktop/src-tauri/gen/schemas/macOS-schema.json"} {
		if err := exec.Command("git", "-C", root, "check-ignore", "-q", "--", p).Run(); err != nil {
			t.Errorf("%s is not ignored: add /gen/schemas to desktop/src-tauri/.gitignore", p)
		}
	}
}
