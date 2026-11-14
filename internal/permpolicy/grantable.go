package permpolicy

import "slices"

// Saved grants (ADR-0055 section 8, docs/plans/active/saved-grants.md): a
// human may save "allow this exact command in this project". Whether a
// request may be OFFERED that choice, and whether a stored grant may be
// APPLIED to it, is one predicate, decided here beside the rules it must never
// outrank: Decision.Grantable. The engine sets it only on an `ask` that is
// plain (no rule matched, or a segment no allow rule covers) for a command it
// parsed completely, whose every segment is free of dynamic words, and none of
// whose programs is on the never-grant list below. Every other request (a
// floor hit, a rule deny or ask, an unparsed or truncated command, a command
// the engine cannot see into) is not grantable, so it asks every time.

// neverGrantPrograms are the programs no saved grant may cover, by canonical
// program name: clients that reach the network, programs that destroy or
// rewrite data or permissions, privilege and wrapper programs that run another
// command the engine would have to see through, and tools whose job is to
// fetch and run code. A deny-list is only as good as its list: the structural
// limits (exact command, no dynamic word, no heredoc, no redirect target the
// floor did not already judge) hold for programs nobody thought to list.
var neverGrantPrograms = []string{
	// network clients
	"curl", "wget", "fetch", "aria2c", "ssh", "scp", "sftp", "ftp", "telnet", "nc", "ncat", "netcat", "socat", "rsync",
	"gh", "hub", "docker", "podman", "kubectl", "helm", "terraform", "aws", "gcloud", "az", "wrangler", "cf", "vercel", "netlify", "fly", "heroku",
	"ping", "dig", "nslookup", "mail", "sendmail",
	// destructive or permission-changing
	"rm", "rmdir", "unlink", "shred", "dd", "mkfs", "fdisk", "parted", "truncate", "mv", "chmod", "chown", "chgrp", "setfacl", "chattr",
	"kill", "killall", "pkill", "shutdown", "reboot", "halt", "diskutil", "launchctl", "systemctl", "crontab", "defaults",
	// privilege and wrappers that run another command
	"sudo", "doas", "su", "env", "xargs", "nohup", "timeout", "time", "nice", "ionice", "watch", "exec", "command", "builtin", "setsid", "stdbuf", "script", "parallel", "find",
	"sh", "bash", "zsh", "fish", "dash", "ksh", "csh", "tcsh", "pwsh", "powershell", "cmd", "eval", "source", "python", "python3", "node", "ruby", "perl", "php", "osascript", "deno", "bun",
	// fetch and run code
	"npx", "pnpx", "bunx", "uvx", "pipx", "brew", "apt", "apt-get", "dnf", "yum", "pacman", "apk", "snap", "winget", "choco", "scoop", "gem", "composer", "nuget",
	// the smind CLI itself: its human-only verbs, and anything that talks to the daemon
	"smind",
}

// neverGrantVerbs are subcommands that fetch or install, for programs that are
// otherwise everyday (a build or test run).
var neverGrantVerbs = map[string][]string{
	"npm":    {"install", "i", "add", "ci", "update", "upgrade", "publish", "login", "exec", "x", "link", "unlink", "uninstall", "remove", "rm", "run-script"},
	"pnpm":   {"install", "i", "add", "update", "up", "upgrade", "publish", "login", "exec", "dlx", "link", "unlink", "uninstall", "remove", "rm"},
	"yarn":   {"install", "add", "up", "upgrade", "publish", "login", "exec", "dlx", "link", "unlink", "remove", "global"},
	"pip":    {"install", "download", "uninstall", "wheel", "config"},
	"pip3":   {"install", "download", "uninstall", "wheel", "config"},
	"uv":     {"add", "sync", "pip", "tool", "run", "python", "publish", "remove", "lock", "venv", "init", "self"},
	"cargo":  {"install", "add", "update", "publish", "login", "fetch", "uninstall", "remove", "yank", "owner"},
	"go":     {"get", "install", "mod", "work", "env", "clean", "generate", "telemetry"},
	"git":    {}, // git commands carry their own rule table (GitRule); the empty list only marks the program as known
	"make":   {},
	"task":   {},
	"gradle": {"publish", "uploadArchives"},
	"mvn":    {"deploy", "release", "dependency:get"},
}

// grantableSegments reports whether every segment of a plain-ask command may be
// covered by a saved grant (see the package comment above).
func grantableSegments(req Request, segs []Segment, cwd string) bool {
	if len(segs) == 0 || req.Worktree == "" {
		return false
	}
	for _, s := range segs {
		if s.Dynamic || s.Inspected || len(s.Argv) == 0 {
			return false
		}
		fs := newFloorSeg(req, s, cwd)
		// A wrapper peeled off argv means the command runs something else.
		if len(fs.argv) == 0 || len(fs.argv) != len(s.Argv) {
			return false
		}
		name := fs.name()
		if slices.Contains(neverGrantPrograms, name) {
			return false
		}
		if verbs, listed := neverGrantVerbs[name]; listed && len(verbs) > 0 {
			// Any word that is a fetch or install verb: the verb is usually the
			// first non-flag word, but a flag value could precede it.
			for _, w := range fs.argv[1:] {
				if slices.Contains(verbs, w) {
					return false
				}
			}
		}
	}
	return true
}
