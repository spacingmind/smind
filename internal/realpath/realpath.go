// Package realpath resolves a path to its final on-disk location, following
// every kind of link the OS has. Sandbox checks ("does this path stay
// inside the worktree?") must use it instead of filepath.EvalSymlinks.
//
// On Windows, Go 1.23+ no longer reports directory junctions (mount points)
// as symlinks, and filepath.EvalSymlinks fails with "path not found" on a
// path through one instead of resolving it. Every escape check built on
// EvalSymlinks therefore let a junction -- which any user can create with
// `mklink /J`, no privilege needed -- point outside the root. Resolve asks
// Windows for the handle's final path instead, which follows symlinks and
// junctions alike.
package realpath

// Resolve returns path with every link resolved, like filepath.EvalSymlinks.
// A missing path yields an error matching fs.ErrNotExist, so callers that
// walk up to the deepest existing ancestor keep working unchanged. Callers
// should pass absolute paths: the Windows implementation always returns an
// absolute path.
func Resolve(path string) (string, error) {
	return resolve(path)
}
