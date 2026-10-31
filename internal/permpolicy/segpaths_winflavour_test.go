package permpolicy

import "testing"

func TestSegmentPathsWindows_NonPathWords(t *testing.T) {
	t.Parallel()
	pl, _ := winEnv(t)
	const cwd = `C:\Work\Wt`
	words := []string{"https://example.com/a/b", "ssh://git@h/x.git", "HEAD:refs/heads/x"}
	for _, cmd := range []string{
		`git push origin HEAD:refs/heads/x`,
		`curl https://example.com/a/b`,
		`git clone ssh://git@h/x.git`,
		`git commit -m "feat: x"`,
	} {
		paths, err := segmentPaths(pl, splitCommand(cmd, true).Segments[0], cwd)
		if err != nil {
			t.Errorf("%q: unexpected error %v", cmd, err)
		}
		for _, p := range paths {
			for _, w := range words {
				if p == w {
					t.Errorf("%q: URL word %q returned as a path", cmd, w)
				}
			}
		}
	}
	for _, cmd := range []string{`go test C:foo`, `del C:\a\b:stream`, `type \\.\pipe\x`, `go test "D:\x|y"`} {
		if _, err := segmentPaths(pl, splitCommand(cmd, true).Segments[0], cwd); err == nil {
			t.Errorf("%q: want an error", cmd)
		}
	}
}
