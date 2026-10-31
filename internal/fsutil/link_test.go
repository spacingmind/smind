package fsutil

import (
	"io/fs"
	"testing"
)

func TestIsLink(t *testing.T) {
	cases := []struct {
		name string
		mode fs.FileMode
		want bool
	}{
		{"regular file", 0o644, false},
		{"directory", fs.ModeDir | 0o755, false},
		{"symlink", fs.ModeSymlink | 0o777, true},
		// Go reports an NTFS junction as ModeIrregular with IsDir() false.
		{"windows junction", fs.ModeIrregular | 0o666, true},
	}
	for _, c := range cases {
		if got := IsLink(c.mode); got != c.want {
			t.Errorf("%s: IsLink(%v) = %v, want %v", c.name, c.mode, got, c.want)
		}
	}
}
