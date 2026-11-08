package permpolicy

import "strings"

// HumanOnlyRPCs are the daemon methods only the human may call: they answer,
// approve or reveal on behalf of a run, so an agent calling one decides its
// own request. The daemon refuses an identified run-scoped connection for each
// (and a test keeps them off RunScopedMethods); a connection that never
// identifies counts as the human's, which is why the floor also refuses a
// command that names one (ADR-0056 revisits the principal). The two answer
// methods of the agent-questions work are listed ahead of their handlers.
var HumanOnlyRPCs = []string{
	"run.respondPermission",
	"run.answerPermission",
	"run.answerQuestion",
	"inbox.resolve",
	"permission.revealHeredoc",
	"run.revealToolHeredoc",
}

// humanOnlyCLI maps a top-level `smind` command to the verbs of it that are
// human-only; a nil list is every verb of the command.
var humanOnlyCLI = map[string][]string{
	"task":       {"approve", "deny", "answer"},
	"permission": nil,
}

// HumanOnlyCLI reports whether args (what follows the smind binary: flags are
// skipped, a flag's own value is not) run a human-only command. The CLI
// refuses these in an agent run and the floor denies them in an agent's
// shell, from this one table.
func HumanOnlyCLI(args []string) bool {
	words := nonFlags(args)
	if len(words) == 0 {
		return false
	}
	verbs, ok := humanOnlyCLI[words[0]]
	if !ok {
		return false
	}
	if verbs == nil {
		return true
	}
	return len(words) > 1 && containsWord(verbs, words[1])
}

// RenamedHumanOnly is the part of the table matched even when the binary's
// name is not smind (a copy or a symlink under another name): the verbs that
// no other tool spells `task approve|deny|answer`.
func renamedHumanOnly(args []string) bool {
	return len(args) >= 2 && args[0] == "task" && containsWord(humanOnlyCLI["task"], args[1])
}

// harmlessReaders print or search their arguments; `echo smind task approve`
// is not a call.
var harmlessReaders = map[string]bool{
	"echo": true, "printf": true, "grep": true, "egrep": true, "fgrep": true, "rg": true,
	"cat": true, "less": true, "head": true, "tail": true, "man": true, "which": true,
	"type": true, "git": true, "gh": true, "ag": true, "sed": true, "awk": true, "diff": true,
}

// selfApproveRules denies a segment that runs a human-only smind command: the
// effective argv (after env, command, exec, xargs, nohup and the like) names
// the smind binary, quoted or by path or computed ($(which smind)), and the
// human-only command, or is any binary run as `task approve|deny|answer`.
func (fs *floorSeg) selfApproveRules() (FloorHit, bool) {
	if len(fs.argv) < 2 {
		return FloorHit{}, false
	}
	name := fs.name()
	if harmlessReaders[name] {
		return FloorHit{}, false
	}
	args := fs.argv[1:]
	switch {
	case name == "smind" || strings.Contains(strings.ToLower(name), "smind") || dynamicWord(fs.argv[0]):
		if HumanOnlyCLI(args) {
			return floorHit("no-self-approve")
		}
	case renamedHumanOnly(args):
		return floorHit("no-self-approve")
	}
	return FloorHit{}, false
}

// namesHumanOnlyRPC reports whether the raw command text names a human-only
// method anywhere: an argument, a quoted JSON frame, or a heredoc body that
// feeds python, nc or a websocket client. It reads the raw text on purpose:
// the segments never carry a heredoc body.
func namesHumanOnlyRPC(raw string) bool {
	for _, m := range HumanOnlyRPCs {
		if strings.Contains(raw, m) {
			return true
		}
	}
	return false
}
