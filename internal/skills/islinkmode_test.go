package skills

import (
	"os"
	"testing"
)

func TestIsLinkMode(t *testing.T) {
	cases := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{"regular file", 0o644, false},
		{"directory", os.ModeDir | 0o755, false},
		{"symlink", os.ModeSymlink | 0o777, true},
		// Go reports an NTFS junction (mount point) as ModeIrregular since 1.23.
		{"windows junction", os.ModeIrregular | os.ModeDir, true},
	}
	for _, c := range cases {
		if got := IsLinkMode(c.mode); got != c.want {
			t.Errorf("%s: IsLinkMode(%v) = %v, want %v", c.name, c.mode, got, c.want)
		}
	}
}
