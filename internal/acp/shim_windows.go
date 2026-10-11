//go:build windows

package acp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// shimUnsafeChars are the characters Go's os/exec quoting cannot make safe
// when the spawned command resolves to a .cmd/.bat batch shim: cmd.exe
// re-parses the whole command line with its own rules before the target
// process ever sees it, and for each of these it either expands, splits,
// or silently drops them (measured on Go 1.27 / Windows 11: %VAR% expands,
// & | < > split or redirect the line, ^ is eaten, CR/LF start a new
// command). Go neither escapes nor rejects them for batch targets, so a
// "successful" spawn could execute something other than what was asked.
const shimUnsafeChars = `%&|<>^` + "\r\n"

// validateShimCommand refuses to spawn a batch shim with any argument
// containing a cmd.exe metacharacter, naming the offending argument.
// Refusing is deliberate: unlike an .exe target (where stdlib
// CommandLineToArgvW quoting round-trips everything measured), a batch
// argument cannot be escaped reliably -- caret escaping per the BatBadBut
// rules is best-effort against a parser whose behavior depends on
// neighboring arguments -- so never-run beats run-something-else. Real
// smind commands (npx -y glm-acp-agent@x, codex app-server) contain none
// of these characters.
func validateShimCommand(command []string) error {
	path, err := exec.LookPath(command[0])
	if err != nil {
		return nil // cmd.Start will surface the lookup error itself
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
	default:
		return nil
	}
	for _, arg := range command[1:] {
		if strings.ContainsAny(arg, shimUnsafeChars) {
			return fmt.Errorf("acp: refusing to spawn batch shim %s with unsafe argument %q: cmd.exe would reinterpret it; use the real executable or drop the metacharacter", path, arg)
		}
	}
	return nil
}
