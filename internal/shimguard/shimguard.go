// Package shimguard stops smind from spawning an npm-style batch shim
// (.cmd/.bat) with an argument cmd.exe would reinterpret.
//
// On Windows, tools installed through npm (npx, claude, codex, kimi) are
// .cmd shims, and Go's os/exec runs them through cmd.exe. Go applies
// CommandLineToArgvW quoting, which round-trips every argument for an .exe
// target but NOT through cmd.exe's second parse. Measured on Go 1.27 /
// Windows 11: `%VAR%` is expanded and split, `& | < >` split or redirect the
// line (the text after `&` runs as another command), `^` is eaten, and CR/LF
// start a new command -- and os/exec neither escapes nor rejects any of it
// for batch targets, so a "successful" spawn could execute something other
// than what was asked. Refusing beats best-effort escaping (whose rules
// depend on neighboring arguments); an .exe target is unaffected.
package shimguard

// Validate returns an error when command[0] resolves to a batch shim and one
// of its arguments contains a cmd.exe metacharacter. The error names the
// argument; prefix (e.g. "acp") namespaces it like the caller's other
// errors. A command that doesn't resolve is not an error here: the real
// spawn reports the lookup failure itself. Off Windows it never errors.
func Validate(prefix string, command []string) error {
	return validate(prefix, command)
}
