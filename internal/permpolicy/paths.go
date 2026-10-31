package permpolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ResolvePath makes p absolute and resolves every symlink in it, so a `..`
// or link that escapes a rule's paths.within is visible before the
// comparison (ADR-0047 §2 step 2). A path that does not exist yet is
// resolved through its deepest existing ancestor with the missing tail
// re-joined: an agent about to write a file names the file, not its parent.
//
// Any other resolution failure is returned as an error; the caller treats
// an unresolvable path as `ask`, never as inside a root.
func ResolvePath(p, cwd string) (string, error) { return hostPlatform().resolve(p, cwd) }

func (pl *platform) resolve(p, cwd string) (string, error) {
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("resolve path: %q contains a NUL byte", p)
	}
	p, err := pl.expandHome(p)
	if err != nil {
		return "", err
	}
	norm, err := pl.normalize(p)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", p, err)
	}
	p = norm
	if !pl.isAbs(p) {
		if cwd == "" {
			return "", fmt.Errorf("resolve path: %q is relative and no cwd was given", p)
		}
		ncwd, err := pl.normalize(cwd)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", cwd, err)
		}
		p = pl.join(ncwd, p)
	}
	p = pl.clean(p)

	// fs.Resolve fails with fs.ErrNotExist for a missing tail and for
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
		resolved, err := pl.fs.Resolve(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = pl.join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve path %q: %w", p, err)
		}
		// Go reports a Windows junction as irregular, not as a symlink.
		if info, lerr := pl.fs.Lstat(cur); lerr == nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if hops >= maxLinkHops {
				return "", fmt.Errorf("resolve path %q: more than %d link hops", p, maxLinkHops)
			}
			hops++
			target, rerr := pl.fs.Readlink(cur)
			if rerr != nil {
				return "", fmt.Errorf("resolve path %q: read link %q: %w", p, cur, rerr)
			}
			if !pl.isAbs(target) {
				target = pl.join(pl.dir(cur), target)
			}
			// tail is stacked deepest-first; re-join from the last entry
			// down so the components keep their order under the target.
			for i := len(tail) - 1; i >= 0; i-- {
				target = pl.join(target, tail[i])
			}
			cur = pl.clean(target)
			tail = nil
			continue
		}
		parent := pl.dir(cur)
		if parent == cur {
			return "", fmt.Errorf("resolve path %q: %w", p, err)
		}
		tail = append(tail, pl.base(cur))
		cur = parent
	}
}

// homeDir is the user's home directory under this flavour. os.UserHomeDir
// honours t.Setenv("HOME", ...), which is what the isolated test homes rely on.
func (pl *platform) homeDir() (string, error) {
	if !pl.windows {
		return os.UserHomeDir()
	}
	if h := pl.getenv("USERPROFILE"); h != "" {
		return h, nil
	}
	return "", errors.New("%USERPROFILE% is not set")
}

var (
	posixHomeForms = []string{"~", "$HOME", "${HOME}"}
	winHomeForms   = []string{"~", "$HOME", "${HOME}", "$USERPROFILE", "${USERPROFILE}", "%USERPROFILE%", "%userprofile%", "$env:USERPROFILE"}
)

// expandHome rewrites a leading ~, $HOME or ${HOME} (on Windows also the
// USERPROFILE forms) to the user's home dir. Only the leading form: a $VAR
// anywhere else is a dynamic word the splitter flags, not something path
// resolution guesses at.
func (pl *platform) expandHome(p string) (string, error) {
	forms := posixHomeForms
	if pl.windows {
		forms = winHomeForms
	}
	for _, form := range forms {
		if !strings.HasPrefix(p, form) {
			continue
		}
		rest := p[len(form):]
		if rest != "" && rest[0] != '/' && !(pl.windows && rest[0] == '\\') {
			continue
		}
		home, err := pl.homeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for %q: %w", p, err)
		}
		return pl.join(home, rest), nil
	}
	return p, nil
}

// Within reports whether resolved is the root itself or lies under it, for
// any of roots. Roots are resolved the same way as the path, so a rule
// written through a link is compared after resolution; a root that cannot be
// resolved is ignored (a scope nobody can locate confines nothing). The
// separator requirement keeps a sibling from passing on a shared prefix:
// /a/bc is not within /a/b.
func Within(resolved string, roots []string) bool { return hostPlatform().within(resolved, roots) }

func (pl *platform) within(resolved string, roots []string) bool {
	for _, root := range roots {
		r, err := pl.resolve(root, "")
		if err != nil {
			continue
		}
		if pl.under(resolved, r) {
			return true
		}
	}
	return false
}

// homeForms are the leading words that expand to the home directory under this flavour.
func (pl *platform) homeForms() []string {
	if pl.windows {
		return winHomeForms
	}
	return posixHomeForms
}
