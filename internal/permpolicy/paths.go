package permpolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/realpath"
)

// ResolvePath makes p absolute and resolves every symlink in it, so a `..`
// or link that escapes a rule's paths.within is visible before the
// comparison (ADR-0047 §2 step 2). A path that does not exist yet is
// resolved through its deepest existing ancestor with the missing tail
// re-joined: an agent about to write a file names the file, not its parent.
//
// Any other resolution failure is returned as an error; the caller treats
// an unresolvable path as `ask`, never as inside a root.
func ResolvePath(p, cwd string) (string, error) {
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("resolve path: %q contains a NUL byte", p)
	}
	p, err := expandHome(p)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		if cwd == "" {
			return "", fmt.Errorf("resolve path: %q is relative and no cwd was given", p)
		}
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)

	// realpath.Resolve fails with fs.ErrNotExist for a missing tail and for
	// a link whose target is gone; both walk up to the deepest ancestor that
	// does resolve. Everything else (permissions, a loop, ...) is a real
	// failure.
	//
	// A dangling link is walked up like any missing tail unless it is
	// followed first: `wt/link -> /etc/newfile` with the target missing
	// would otherwise be judged as wt/link, inside wt, although a write
	// through it creates /etc/newfile. So an ErrNotExist on a path whose
	// last component is itself a link restarts the loop from the link's
	// target, with the pending tail re-joined. The hop cap bounds a link
	// cycle the OS would refuse with ELOOP but a missing target keeps
	// alive.
	const maxLinkHops = 40
	cur, tail := p, []string{}
	hops := 0
	for {
		resolved, err := realpath.Resolve(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve path %q: %w", p, err)
		}
		if info, lerr := os.Lstat(cur); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			if hops >= maxLinkHops {
				return "", fmt.Errorf("resolve path %q: more than %d link hops", p, maxLinkHops)
			}
			hops++
			target, rerr := os.Readlink(cur)
			if rerr != nil {
				return "", fmt.Errorf("resolve path %q: read link %q: %w", p, cur, rerr)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			cur = filepath.Join(append([]string{target}, tail...)...)
			tail = nil
			continue
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("resolve path %q: %w", p, err)
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// expandHome rewrites a leading ~, $HOME or ${HOME} to the user's home dir.
// Only the leading form: a $VAR anywhere else is a dynamic word the splitter
// flags, not something path resolution guesses at. os.UserHomeDir honours
// t.Setenv("HOME", ...), which is what the isolated test homes rely on.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for %q: %w", p, err)
		}
		return filepath.Join(home, p[1:]), nil
	}
	for _, prefix := range []string{"$HOME", "${HOME}"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve home for %q: %w", p, err)
			}
			return filepath.Join(home, p[len(prefix):]), nil
		}
	}
	return p, nil
}

// Within reports whether resolved is the root itself or lies under it, for
// any of roots. Roots are resolved the same way as the path, so a rule
// written through a link is compared after resolution; a root that cannot be
// resolved is ignored (a scope nobody can locate confines nothing). The
// separator requirement keeps a sibling from passing on a shared prefix:
// /a/bc is not within /a/b.
func Within(resolved string, roots []string) bool {
	for _, root := range roots {
		r, err := ResolvePath(root, "")
		if err != nil {
			continue
		}
		if resolved == r || strings.HasPrefix(resolved, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
