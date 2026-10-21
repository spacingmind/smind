package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// WriteLauncher writes an executable `#!/bin/sh` wrapper into dir that records
// its pid in pidFile and then execs realCLI under `/usr/bin/env -i` with
// exactly the NAME=VALUE entries in env: the SDK's transport inherits the
// daemon's whole environment and can only add to it, so an allowlist needs a
// wrapper (WithCLIPath). `exec` keeps the pid, so pidFile holds the CLI's own
// pid and the caller can kill the process tree from it (the SDK only signals
// the CLI itself). It returns the script path.
//
// The env VALUES are written to disk in the script (and are briefly visible
// in env's argv): only ever pass the non-secret allowlist (see AllowlistEnv);
// never add a token or key. dir must be a real directory (not a symlink)
// owned by the caller and not group/world writable; the file is created with
// O_EXCL|O_NOFOLLOW and mode 0700, so a pre-planted file or symlink is an
// error, never followed. dir must never be inside the arm. pidFile must be an
// absolute path inside dir. An entry that is not NAME=VALUE with a valid name,
// or a relative realCLI or dir, is an error; values are single-quoted, so
// they are never interpreted by the shell.
func WriteLauncher(dir, realCLI string, env []string, pidFile string) (string, error) {
	if !filepath.IsAbs(dir) || !filepath.IsAbs(realCLI) || !filepath.IsAbs(pidFile) {
		return "", errors.New("sandbox: launcher dir, real CLI and pid file paths must be absolute")
	}
	if filepath.Dir(filepath.Clean(pidFile)) != filepath.Clean(dir) {
		return "", errors.New("sandbox: pid file must be directly inside the launcher dir")
	}
	if err := checkLauncherDir(dir); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\necho $$ > " + shQuote(pidFile) + "\nexec /usr/bin/env -i")
	for _, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || !envNameRE.MatchString(name) {
			return "", fmt.Errorf("sandbox: launcher env entry %q is not NAME=VALUE", name)
		}
		b.WriteString(" " + name + "=" + shQuote(val))
	}
	b.WriteString(" " + shQuote(realCLI) + " \"$@\"\n")
	path := filepath.Join(dir, "claude-launcher.sh")
	f, err := createExclusive(path)
	if err != nil {
		return "", fmt.Errorf("sandbox: create launcher: %w", err)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("sandbox: write launcher: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("sandbox: write launcher: %w", err)
	}
	return path, nil
}

// checkLauncherDir requires a real, user-owned directory that no one else can
// write to.
func checkLauncherDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("sandbox: launcher dir: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("sandbox: launcher dir is not a real directory")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("sandbox: launcher dir %s is group/world writable (%v)", dir, info.Mode().Perm())
	}
	if !ownedByCurrentUser(info) {
		return errors.New("sandbox: launcher dir is not owned by the current user")
	}
	return nil
}

// shQuote single-quotes s for /bin/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
