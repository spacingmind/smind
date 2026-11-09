package permpolicy

import (
	"regexp"
	"slices"
	"strings"
)

// HumanOnlyRPCs are the daemon methods only the human may call: they answer,
// approve or reveal on behalf of a run, or pair a host or device, so an agent
// calling one decides for itself. The daemon refuses an identified run-scoped
// connection for each (and a test keeps them off RunScopedMethods); a
// connection that never identifies counts as the human's, which is why the
// floor also refuses a command that names one next to a transport. The two
// answer methods of the agent-questions work are named ahead of their
// handlers (see pendingHumanOnlyRPCs).
var HumanOnlyRPCs = []string{
	"run.respondPermission",
	"run.answerPermission",
	"run.answerQuestion",
	"inbox.resolve",
	"permission.revealHeredoc",
	"run.revealToolHeredoc",
	"fleet.host.approve",
	"fleet.host.reject",
	"relay.device.approve",
	"relay.device.revoke",
	"chat.fork",
	"chat.forkSeed",
	"profile.create",
	"profile.update",
}

// PendingHumanOnlyRPCs are the entries of HumanOnlyRPCs whose handlers are not
// on this branch yet; the wsapi drift test lets them be absent and drops the
// tolerance for each as soon as its handler exists.
var PendingHumanOnlyRPCs = []string{"run.answerPermission"}

// humanOnlyCommand is one human-only smind command: the words that name it
// and the verbs of it that are human-only (nil: every verb).
type humanOnlyCommand struct {
	path  []string
	verbs []string
	// flag, when set, makes the command human-only only with that flag (`--flag` or
	// `--flag=value`): `smind profile seed roles` reads, `--apply` rewrites role prompts.
	flag string
	// bare marks a command no other tool spells: it is also matched without the
	// binary's name (a renamed copy, `go run`, text handed to a shell).
	bare bool
}

var humanOnlyCLI = []humanOnlyCommand{
	{path: []string{"task"}, verbs: []string{"approve", "deny", "answer"}, bare: true},
	{path: []string{"permission"}},
	{path: []string{"host"}, verbs: []string{"approve", "reject"}},
	{path: []string{"relay", "devices"}, verbs: []string{"approve", "revoke"}},
	{path: []string{"task", "chat"}, verbs: []string{"fork"}, bare: true},
	{path: []string{"profile"}, verbs: []string{"add", "edit"}, bare: true},
	{path: []string{"profile", "seed"}, flag: "--apply", bare: true},
}

// HumanOnlyCLI reports whether args (what follows the smind binary: flags are
// skipped) run a human-only command. The CLI refuses these in an agent run and
// the floor denies them in an agent's shell, from this one table.
func HumanOnlyCLI(args []string) bool {
	words := nonFlags(args)
	for _, c := range humanOnlyCLI {
		if len(words) < len(c.path) || !slices.Equal(words[:len(c.path)], c.path) {
			continue
		}
		switch {
		case c.flag != "":
			if hasFlag(args, c.flag) {
				return true
			}
		case c.verbs == nil:
			return true
		case len(words) > len(c.path) && containsWord(c.verbs, words[len(c.path)]):
			return true
		}
	}
	return false
}

// hasFlag reports `flag` or `flag=value` among args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// dynamicHumanOnly reports a command group or verb the shell computes
// (`smind $x approve`, `smind task $(echo approve)`): it may be anything.
func dynamicHumanOnly(args []string) bool {
	words := nonFlags(args)
	if len(words) == 0 {
		return false
	}
	if dynamicWord(words[0]) {
		return true
	}
	for _, c := range humanOnlyCLI {
		if len(words) <= len(c.path) || !slices.Equal(words[:len(c.path)], c.path) {
			continue
		}
		if dynamicWord(words[len(c.path)]) {
			return true
		}
		// A flag-gated command (`profile seed roles --apply`) may get its flag
		// from the shell: any computed word after the path could be it.
		if c.flag != "" && slices.ContainsFunc(words[len(c.path):], dynamicWord) {
			return true
		}
	}
	return false
}

// namedBare reports the command spelled as adjacent words anywhere in args,
// without the binary's name: how a renamed copy, `go run ./cmd/smind` or a
// launcher that takes the real command as arguments writes it.
func (c humanOnlyCommand) namedBare(args []string) bool {
	for i := 0; i+len(c.path) <= len(args); i++ {
		if !slices.Equal(args[i:i+len(c.path)], c.path) {
			continue
		}
		rest := args[i+len(c.path):]
		switch {
		case c.flag != "":
			if hasFlag(args, c.flag) {
				return true
			}
		case c.verbs == nil:
			return true
		case len(rest) > 0 && containsWord(c.verbs, rest[0]):
			return true
		}
	}
	return false
}

// bareVerbPair reports a command of the table that is safe to match without
// the binary's name (the bare rows).
func bareVerbPair(args []string) bool {
	for _, c := range humanOnlyCLI {
		if c.bare && c.namedBare(args) {
			return true
		}
	}
	return false
}

// pattern is the command as text: `smind… <path> <verb>` (without the binary
// for a bare row), or `<path> … <flag>` for a flag row.
func (c humanOnlyCommand) pattern() string {
	words := make([]string, len(c.path))
	for i, w := range c.path {
		words[i] = regexp.QuoteMeta(w)
	}
	p := strings.Join(words, `\s+`)
	switch {
	case c.flag != "":
		p += `\b[^\n]*?` + regexp.QuoteMeta(c.flag) + `(?:[\s='"]|$)`
	case c.verbs != nil:
		vs := make([]string, len(c.verbs))
		for i, v := range c.verbs {
			vs[i] = regexp.QuoteMeta(v)
		}
		p += `\s+(?:` + strings.Join(vs, "|") + `)\b`
	default:
		p += `\b`
	}
	if !c.bare {
		p = `smind\S*\s+` + p
	}
	return p
}

// humanOnlyText finds a human-only smind command written out as text: inside a
// script handed to a command runner (`su -c '...'`, `eval '...'`, `ssh host
// '...'`) or piped to a shell. Built from the one table, so a new human-only
// command cannot miss it.
var humanOnlyText = buildHumanOnlyText()

func buildHumanOnlyText() *regexp.Regexp {
	alts := make([]string, len(humanOnlyCLI))
	for i, c := range humanOnlyCLI {
		alts[i] = c.pattern()
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(alts, "|") + `)`)
}

// commandRunners take a command, or a script, as arguments and run it; the
// effective argv does not name it as a command word.
var commandRunners = map[string]bool{
	"su": true, "script": true, "ssh": true, "eval": true, "watch": true, "flock": true,
	"find": true, "parallel": true, "chroot": true, "unshare": true, "nsenter": true, "runuser": true,
	"sudo": true, "doas": true, "setpriv": true, "xterm": true, "tmux": true, "screen": true,
	"open": true, "osascript": true, "at": true, "batch": true, "cron": true, "crontab": true,
}

// harmlessReaders print or search their arguments; `echo smind task approve`
// is not a call.
var harmlessReaders = map[string]bool{
	"echo": true, "printf": true, "grep": true, "egrep": true, "fgrep": true, "rg": true,
	"cat": true, "less": true, "head": true, "tail": true, "man": true, "which": true,
	"type": true, "ag": true, "diff": true,
}

// selfApproveRules denies a segment that runs a human-only smind command: the
// effective argv (after env, command, exec, xargs, nohup and the like) names
// the smind binary, quoted or by path or computed ($(which smind)), and the
// human-only command; or is any binary run with `task approve|deny|answer`;
// or is a command runner given such a command as text.
func (fs *floorSeg) selfApproveRules() (FloorHit, bool) {
	if len(fs.argv) < 2 {
		return FloorHit{}, false
	}
	name := fs.name()
	if harmlessReaders[name] {
		return FloorHit{}, false
	}
	args := fs.argv[1:]
	smindish := name == "smind" || strings.Contains(strings.ToLower(name), "smind") || dynamicWord(fs.argv[0])
	switch {
	case smindish && (HumanOnlyCLI(args) || dynamicHumanOnly(args)):
		return floorHit("no-self-approve")
	case !smindish && bareVerbPair(args):
		return floorHit("no-self-approve")
	case commandRunners[name] && humanOnlyText.MatchString(strings.Join(args, " ")):
		return floorHit("no-self-approve")
	}
	return FloorHit{}, false
}

// shellFedHumanOnly reports a bare shell (no -c, no script: it reads its
// commands from stdin, a here-string or a pipe) in a command whose raw text
// writes a human-only smind command: `echo 'smind task approve 1' | sh`.
func shellFedHumanOnly(segs []*floorSeg, raw string) bool {
	if !humanOnlyText.MatchString(raw) {
		return false
	}
	for _, fs := range segs {
		if len(fs.argv) == 0 || !shellNames[fs.name()] {
			continue
		}
		bare := true
		for _, a := range fs.argv[1:] {
			if a == "-c" || shortCluster(a, "c") || (!strings.HasPrefix(a, "-") && a != "-") {
				bare = false
			}
		}
		if bare {
			return true
		}
	}
	return false
}

var shellNames = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// transportNames speak to a socket or run a program that can: a human-only
// method named in a command with one of them is a call, not a search.
var transportNames = map[string]bool{
	"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true, "socat": true, "telnet": true,
	"websocat": true, "wscat": true, "openssl": true,
	"node": true, "deno": true, "bun": true, "ruby": true, "perl": true, "php": true, "lua": true,
}

// hasTransport reports a segment that is a network client or an interpreter.
func hasTransport(segs []*floorSeg) bool {
	for _, fs := range segs {
		if len(fs.argv) == 0 {
			continue
		}
		name := fs.name()
		if transportNames[name] || strings.HasPrefix(name, "python") || name == "go" && containsWord(fs.argv[1:], "run") {
			return true
		}
	}
	return false
}

// namesHumanOnlyRPC reports whether the raw command text names a human-only
// method anywhere: an argument, a quoted JSON frame, or a heredoc body that
// feeds python, nc or a websocket client. It reads the raw text on purpose:
// the segments never carry a heredoc body. The caller pairs it with
// hasTransport, so `rg run.respondPermission` and a commit message are not
// calls.
func namesHumanOnlyRPC(raw string) bool {
	for _, m := range HumanOnlyRPCs {
		if strings.Contains(raw, m) {
			return true
		}
	}
	return false
}
