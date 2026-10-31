// Package fsutil holds small filesystem predicates shared by production code.
package fsutil

import "io/fs"

// IsLink reports whether mode (from os.Lstat or fs.DirEntry.Type) describes a
// symlink or, on Windows, a junction.
//
// Since Go 1.23 an NTFS junction (mount point) is reported as
// fs.ModeIrregular, not fs.ModeSymlink, and with IsDir() false. A junction
// redirects a path exactly as a symlink does and any user can create one
// without privilege, so every "refuse links" check must use IsLink rather
// than testing fs.ModeSymlink alone (a drift test enforces this).
//
// Archive entry modes (tar/zip headers) are not filesystem objects and keep
// testing fs.ModeSymlink directly.
func IsLink(mode fs.FileMode) bool {
	return mode&(fs.ModeSymlink|fs.ModeIrregular) != 0
}
