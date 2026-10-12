// Package claudecli resolves and launches the Claude Code CLI on native
// Windows (ADR-0029).
//
// Two Windows facts make the SDK's defaults unusable there:
//
//   - claudecode.New finds the CLI with exec.LookPath("claude"), which on a
//     machine with the npm install returns claude.cmd. Go runs a .cmd
//     through cmd.exe, which re-parses the arguments (the --mcp-config path,
//     & % ^ ...), so smind must hand the SDK the real claude.exe instead.
//   - The SDK stops the CLI with Process.Kill on that one process, which
//     orphans everything the CLI spawned (its Bash tool commands, MCP
//     servers). smind therefore starts the CLI through itself in "launcher
//     mode" (launch_windows.go): the launcher creates a kill-on-close Job
//     Object around the real CLI, so killing the launcher kills the tree.
//
// Resolve is pure over injected functions and untagged so every platform's
// CI runs its tests.
package claudecli

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// shimExeRe finds the real executable a npm .cmd shim runs:
//
//	"%dp0%\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*
//
// (npm writes %dp0% or %~dp0 for the shim's own directory).
var shimExeRe = regexp.MustCompile(`(?i)%~?dp0%?[\\/]+([^"\r\n%]*?claude\.exe)`)

// npmExeRel is where the npm package keeps its native binary, relative to
// the directory holding the shim.
const npmExeRel = `node_modules\@anthropic-ai\claude-code\bin\claude.exe`

// Resolve returns the absolute path of a real claude.exe, never a .cmd/.bat
// shim. lookPath is exec.LookPath, readFile reads a shim, exists reports
// whether a file exists, and home is the user's home directory (may be
// empty). The error names everything that was tried.
func Resolve(lookPath func(string) (string, error), readFile func(string) ([]byte, error), exists func(string) bool, home string) (string, error) {
	var tried []string

	if found, err := lookPath("claude"); err == nil {
		switch strings.ToLower(winExt(found)) {
		case ".exe":
			if isWinAbs(found) {
				return found, nil
			}
			tried = append(tried, found+" (not an absolute path)")
		case ".cmd", ".bat":
			if exe, ok := exeBehindShim(found, readFile, exists); ok {
				return exe, nil
			}
			tried = append(tried, found+" (npm shim; no claude.exe behind it)")
		default:
			tried = append(tried, found+" (not an .exe)")
		}
	} else {
		tried = append(tried, "claude on PATH")
	}

	if home != "" {
		for _, rel := range []string{`.local\bin\claude.exe`, `.claude\local\claude.exe`} {
			candidate := winJoin(home, rel)
			if exists(candidate) {
				return candidate, nil
			}
			tried = append(tried, candidate)
		}
	}

	return "", fmt.Errorf("claudecli: could not find a native claude.exe (tried: %s); install Claude Code (https://claude.com/claude-code, or `npm install -g @anthropic-ai/claude-code`) so that claude.exe is on PATH", strings.Join(tried, ", "))
}

// exeBehindShim resolves the claude.exe a npm shim launches: first the path
// written in the shim itself, then the layout npm uses.
func exeBehindShim(shim string, readFile func(string) ([]byte, error), exists func(string) bool) (string, bool) {
	dir := winDir(shim)
	if body, err := readFile(shim); err == nil {
		if m := shimExeRe.FindSubmatch(body); m != nil {
			candidate := winJoin(dir, strings.ReplaceAll(string(m[1]), "/", `\`))
			if exists(candidate) {
				return candidate, true
			}
		}
	}
	candidate := winJoin(dir, npmExeRel)
	if exists(candidate) {
		return candidate, true
	}
	return "", false
}

// ErrNotAClaudeExe is returned by ValidateTarget for anything the launcher
// must not execute.
var ErrNotAClaudeExe = errors.New("claudecli: launch target must be an absolute path to claude.exe")

// ValidateTarget is the launcher's gate: only an absolute path whose file
// name is claude.exe is ever executed, so the launch env var cannot be used
// to run an arbitrary program through smind.
func ValidateTarget(target string) error {
	if !isWinAbs(target) || !strings.EqualFold(winBase(target), "claude.exe") {
		return fmt.Errorf("%w (got %q)", ErrNotAClaudeExe, target)
	}
	return nil
}

// Windows path helpers. filepath's behaviour depends on the host OS, but
// Resolve must be testable (and give the same answers) everywhere, so these
// treat both `\` and `/` as separators regardless of GOOS.

func lastSep(p string) int { return strings.LastIndexAny(p, `\/`) }

func winBase(p string) string {
	if i := lastSep(p); i >= 0 {
		return p[i+1:]
	}
	return p
}

func winDir(p string) string {
	if i := lastSep(p); i >= 0 {
		return p[:i]
	}
	return "."
}

func winExt(p string) string {
	base := winBase(p)
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i:]
	}
	return ""
}

func winJoin(dir, rel string) string {
	return strings.TrimRight(dir, `\/`) + `\` + strings.TrimLeft(rel, `\/`)
}

// isWinAbs reports a drive-letter path (C:\x or C:/x) or a UNC path.
func isWinAbs(p string) bool {
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0] | 0x20
		return c >= 'a' && c <= 'z'
	}
	return strings.HasPrefix(p, `\\`)
}
