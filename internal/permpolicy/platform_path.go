package permpolicy

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/spacingmind/smind/internal/realpath"
)

// errUnrepresentable marks a path the Windows flavour cannot place (drive-relative, rooted without a drive, device
// namespace, stream, illegal character, trailing dot/space, reserved device name). The engine treats it as ask /
// floor-deny, never as inside a root.
var errUnrepresentable = errors.New("unrepresentable windows path")

// pathFS is the filesystem seam of the path layer, so the Windows flavour runs against a fake filesystem on any OS.
type pathFS interface {
	// Resolve is like realpath.Resolve: a fully resolved absolute path; the error matches fs.ErrNotExist when missing.
	Resolve(p string) (string, error)
	Lstat(p string) (fs.FileInfo, error)
	Readlink(p string) (string, error)
}

type osFS struct{}

func (osFS) Resolve(p string) (string, error)    { return realpath.Resolve(p) }
func (osFS) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }
func (osFS) Readlink(p string) (string, error)   { return os.Readlink(p) }

// platform is the path flavour (POSIX or Windows) every path decision runs through.
type platform struct {
	windows  bool
	fs       pathFS
	getenv   func(string) string
	tempDirs func() []string
}

func posixTempDirs() []string {
	return []string{os.TempDir(), "/tmp", "/private/tmp", "/var/tmp"}
}

// hostPlatform is the flavour of the running OS.
func hostPlatform() *platform {
	if runtime.GOOS == "windows" {
		return &platform{
			windows:  true,
			fs:       osFS{},
			getenv:   os.Getenv,
			tempDirs: func() []string { return []string{os.TempDir()} },
		}
	}
	return posixPlatform(osFS{})
}

func posixPlatform(fs pathFS) *platform {
	return &platform{windows: false, fs: fs, getenv: os.Getenv, tempDirs: posixTempDirs}
}

func windowsPlatform(fs pathFS, env map[string]string) *platform {
	return &platform{
		windows: true,
		fs:      fs,
		getenv:  func(k string) string { return env[k] },
		tempDirs: func() []string {
			var out []string
			for _, k := range []string{"TEMP", "TMP"} {
				if v := env[k]; v != "" {
					out = append(out, v)
				}
			}
			return out
		},
	}
}

func (pl *platform) sep() string {
	if pl.windows {
		return `\`
	}
	return "/"
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

var winReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// winComponentOK validates one path component (not a drive).
func winComponentOK(c string) bool {
	if c == "" || c == "." || c == ".." {
		return true
	}
	if strings.ContainsAny(c, `<>|"*?:`) {
		return false
	}
	if strings.Trim(c, ".") == "" {
		return true
	}
	if strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ") {
		return false
	}
	stem := c
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	return !winReserved[strings.ToUpper(stem)]
}

func winComponentsOK(rest string) bool {
	for _, c := range strings.Split(rest, `\`) {
		if !winComponentOK(c) {
			return false
		}
	}
	return true
}

// normalize maps a Windows word to the canonical form (backslashes, upper-case drive, verbatim and msys prefixes
// resolved). POSIX returns p unchanged.
func (pl *platform) normalize(p string) (string, error) {
	if !pl.windows {
		return p, nil
	}
	switch {
	case len(p) >= 8 && strings.EqualFold(p[:8], `\\?\UNC\`):
		p = `\\` + p[8:]
	case strings.HasPrefix(p, `\\?\`):
		p = p[4:]
		if len(p) < 3 || !isLetter(p[0]) || p[1] != ':' || p[2] != '\\' {
			return "", errUnrepresentable
		}
	}
	if strings.HasPrefix(p, `\\.\`) || strings.HasPrefix(p, "//./") || strings.HasPrefix(p, "//?/") {
		return "", errUnrepresentable
	}
	if lower := strings.ToLower(p); strings.HasPrefix(lower, "/cygdrive/") && len(p) >= 11 && isLetter(p[10]) && (len(p) == 11 || p[11] == '/') {
		p = string(p[10]) + ":/" + strings.TrimPrefix(p[11:], "/")
	} else if len(p) >= 2 && p[0] == '/' && isLetter(p[1]) && (len(p) == 2 || p[2] == '/') {
		p = string(p[1]) + ":/" + strings.TrimPrefix(p[2:], "/")
	}
	p = strings.ReplaceAll(p, "/", `\`)

	switch {
	case strings.HasPrefix(p, `\\`):
		parts := strings.SplitN(p[2:], `\`, 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", errUnrepresentable
		}
		if !winComponentOK(parts[0]) || !winComponentOK(parts[1]) {
			return "", errUnrepresentable
		}
		if len(parts) == 3 && !winComponentsOK(parts[2]) {
			return "", errUnrepresentable
		}
		return p, nil
	case len(p) >= 2 && isLetter(p[0]) && p[1] == ':':
		if len(p) == 2 || p[2] != '\\' {
			return "", errUnrepresentable
		}
		if !winComponentsOK(p[3:]) {
			return "", errUnrepresentable
		}
		return strings.ToUpper(p[:1]) + `:\` + p[3:], nil
	case strings.HasPrefix(p, `\`):
		return "", errUnrepresentable
	}
	if !winComponentsOK(p) {
		return "", errUnrepresentable
	}
	return p, nil
}

// winSplit splits a (separator-normalized) Windows path into its volume, whether it is rooted, and its components.
func winSplit(p string) (vol string, rooted bool, comps []string) {
	rest := p
	switch {
	case strings.HasPrefix(p, `\\`):
		parts := strings.SplitN(p[2:], `\`, 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			vol = `\\` + parts[0] + `\` + parts[1]
			rooted = true
			rest = ""
			if len(parts) == 3 {
				rest = parts[2]
			}
		}
	case len(p) >= 2 && isLetter(p[0]) && p[1] == ':':
		vol = p[:2]
		rest = p[2:]
		if strings.HasPrefix(rest, `\`) {
			rooted = true
		}
	case strings.HasPrefix(p, `\`):
		rooted = true
	}
	for _, c := range strings.Split(rest, `\`) {
		if c != "" {
			comps = append(comps, c)
		}
	}
	return vol, rooted, comps
}

func winBuild(vol string, rooted bool, comps []string) string {
	body := strings.Join(comps, `\`)
	unc := strings.HasPrefix(vol, `\\`)
	switch {
	case unc:
		if body == "" {
			return vol
		}
		return vol + `\` + body
	case rooted:
		return vol + `\` + body
	case body == "":
		if vol != "" {
			return vol
		}
		return "."
	}
	return vol + body
}

func (pl *platform) isAbs(p string) bool {
	if !pl.windows {
		return path.IsAbs(p)
	}
	p = strings.ReplaceAll(p, "/", `\`)
	vol, rooted, _ := winSplit(p)
	if vol == "" {
		return false
	}
	return rooted
}

func (pl *platform) clean(p string) string {
	if !pl.windows {
		return path.Clean(p)
	}
	if p == "" {
		return "."
	}
	p = strings.ReplaceAll(p, "/", `\`)
	vol, rooted, comps := winSplit(p)
	var out []string
	for _, c := range comps {
		switch c {
		case ".":
		case "..":
			if n := len(out); n > 0 && out[n-1] != ".." {
				out = out[:n-1]
			} else if !rooted {
				out = append(out, "..")
			}
		default:
			out = append(out, c)
		}
	}
	return winBuild(vol, rooted, out)
}

func (pl *platform) join(elem ...string) string {
	if !pl.windows {
		return path.Join(elem...)
	}
	acc := ""
	for _, e := range elem {
		if e == "" {
			continue
		}
		switch {
		case acc == "" || pl.isAbs(e):
			acc = e
		default:
			acc = acc + `\` + e
		}
	}
	if acc == "" {
		return ""
	}
	return pl.clean(acc)
}

func (pl *platform) dir(p string) string {
	if !pl.windows {
		return path.Dir(p)
	}
	c := pl.clean(p)
	vol, rooted, comps := winSplit(c)
	if len(comps) == 0 {
		return c
	}
	return winBuild(vol, rooted, comps[:len(comps)-1])
}

func (pl *platform) base(p string) string {
	if !pl.windows {
		return path.Base(p)
	}
	c := pl.clean(p)
	_, _, comps := winSplit(c)
	if len(comps) == 0 {
		if c == "." {
			return "."
		}
		return `\`
	}
	return comps[len(comps)-1]
}

func (pl *platform) volume(p string) string {
	if !pl.windows {
		return ""
	}
	vol, _, _ := winSplit(pl.clean(p))
	return vol
}

// under reports whether p is root or inside it.
func (pl *platform) under(p, root string) bool {
	p, root = pl.clean(p), pl.clean(root)
	if pl.windows {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	if p == root {
		return true
	}
	if strings.HasSuffix(root, pl.sep()) {
		return strings.HasPrefix(p, root)
	}
	return strings.HasPrefix(p, root+pl.sep())
}

func (pl *platform) eq(a, b string) bool {
	a, b = pl.clean(a), pl.clean(b)
	if pl.windows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// cmdName is the command name of argv0: base name, and on Windows lower-cased without a launcher extension.
func (pl *platform) cmdName(argv0 string) string {
	if !pl.windows {
		return path.Base(argv0)
	}
	if i := strings.LastIndexAny(argv0, `/\`); i >= 0 {
		argv0 = argv0[i+1:]
	}
	argv0 = strings.ToLower(argv0)
	for _, ext := range []string{".exe", ".com", ".cmd", ".bat"} {
		if strings.HasSuffix(argv0, ext) {
			return strings.TrimSuffix(argv0, ext)
		}
	}
	return argv0
}

func (pl *platform) isNullDevice(w string) bool {
	if !pl.windows {
		return w == "/dev/null"
	}
	return strings.EqualFold(w, "nul") || strings.EqualFold(w, "/dev/null") || strings.EqualFold(w, `\dev\null`)
}
