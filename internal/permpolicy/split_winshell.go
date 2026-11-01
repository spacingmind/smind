package permpolicy

import "strings"

// Windows script hosts whose -Command / /c payload the splitter parses like
// `sh -c`: powershell, pwsh and cmd. The payload is scanned in its own
// dialect (see scanner.dialect) and its commands appear as ordinary segments,
// so the floor and the rules judge what actually runs. Anything the splitter
// cannot be sure of is reported in Parsed.Unparsed, which makes the request
// ask: an encoded command, a script block, a backtick or caret escape, a
// parenthesised expression, a payload built from outer-shell expansions.

// psRunsText are PowerShell commands that execute text or another program
// the splitter cannot see into.
var psRunsText = map[string]bool{
	"invoke-expression": true, "iex": true, "invoke-command": true, "icm": true,
	"start-process": true, "saps": true, "start": true,
}

// cmdRunsText are cmd keywords that take a nested command as arguments.
var cmdRunsText = map[string]bool{
	"if": true, "for": true, "else": true, "call": true, "start": true, "goto": true,
}

// winShellDialect names the dialect of a script host command (dialectNone for
// anything else). Both path separators are accepted: argv words keep Windows
// backslashes.
func winShellDialect(name string) dialect {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	switch name {
	case "powershell", "pwsh":
		return dialectPS
	case "cmd":
		return dialectCmd
	}
	return dialectNone
}

// winShellScript parses the literal payload of powershell|pwsh -Command and
// cmd /c|/k|/r. A wrapper without a command flag (-File, interactive) is left
// alone and keeps the Windows "runs text the engine cannot see" ask.
func (s *scanner) winShellScript(argv []string, d dialect) {
	var parts []string
	var reason string
	var found bool
	if d == dialectPS {
		parts, found, reason = psPayload(argv)
	} else {
		parts, found, reason = cmdPayload(argv)
	}
	if !found {
		return
	}
	if reason == "" && s.cur.Dynamic {
		reason = d.String() + " command built from an outer-shell expansion"
	}
	var payload string
	if reason == "" {
		payload, reason = joinPayload(parts)
	}
	if reason != "" {
		s.unparsed(reason)
		return
	}
	before := len(s.p.Unparsed)
	if n := s.recurseAs(payload, d, -1); n == 0 && len(s.p.Unparsed) == before {
		// Operator-only text (&&, ;;) would be an error in the script host:
		// the caller must ask, not allow.
		s.unparsed("no command found in the " + d.String() + " script")
		return
	}
	s.cur.Inspected = true
}

// joinPayload turns the words after the command flag back into the text the
// script host sees: one word is the script as written, several are joined by
// spaces (PowerShell and cmd both do), re-quoting a word that held spaces or
// operators so its quoting survives the round trip.
func joinPayload(parts []string) (string, string) {
	const noCommand = "script host command flag with no command (it would read stdin)"
	if len(parts) == 0 {
		return "", noCommand
	}
	if len(parts) == 1 {
		if strings.TrimSpace(parts[0]) == "" || parts[0] == "-" {
			return "", noCommand
		}
		return parts[0], ""
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		if strings.ContainsAny(p, " \t;&|<>(){}") {
			if strings.Contains(p, `"`) {
				return "", "script host command words mix quotes and operators"
			}
			p = `"` + p + `"`
		}
		out[i] = p
	}
	return strings.Join(out, " "), ""
}

// psNoValue are PowerShell options that take no value. psWithValue are those
// that take one (their abbreviations are recognised by optionIs). Any other
// option is unknown: a value-taking option we do not know would shift the
// payload, so the script is not analysed.
var (
	psNoValue = map[string]bool{
		"noprofile": true, "nop": true, "noninteractive": true, "noni": true, "nologo": true,
		"nol": true, "noexit": true, "nox": true, "mta": true, "sta": true, "login": true, "l": true,
	}
	psWithValue = []string{
		"executionpolicy", "windowstyle", "version", "inputformat", "outputformat",
		"workingdirectory", "configurationname", "psconsolefile", "settingsfile", "custompipename",
	}
	// psWithValueShort are the documented short spellings of psWithValue.
	psWithValueShort = map[string]bool{"ep": true, "w": true, "v": true, "wd": true, "o": true, "of": true, "in": true}
)

// optionIs reports whether f (lower-cased, no dash) is an abbreviation of name
// with at least min characters, as PowerShell accepts.
func optionIs(f, name string, min int) bool {
	return len(f) >= min && strings.HasPrefix(name, f)
}

// psTakesValue reports whether PowerShell option f consumes the next word.
func psTakesValue(f string) bool {
	if psWithValueShort[f] {
		return true
	}
	for _, name := range psWithValue {
		if optionIs(f, name, 2) {
			return true
		}
	}
	return false
}

// psPayload finds the words of powershell|pwsh -Command. A first non-option
// word is an implicit -Command, as PowerShell reads it.
func psPayload(argv []string) (parts []string, found bool, reason string) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == "-" {
			return nil, true, "PowerShell reads its command from stdin"
		}
		if len(a) < 2 || a[0] != '-' {
			rest := argv[i:]
			for _, w := range rest[1:] {
				if len(w) > 1 && w[0] == '-' && optionIs(strings.ToLower(strings.TrimLeft(w, "-")), "command", 1) {
					return nil, true, "PowerShell arguments are ambiguous (a -Command after the command text)"
				}
			}
			return rest, true, ""
		}
		f := strings.ToLower(strings.TrimLeft(a, "-"))
		switch {
		case f == "e" || f == "ec" || optionIs(f, "encodedcommand", 2):
			return nil, true, "PowerShell -EncodedCommand cannot be decoded"
		case optionIs(f, "command", 1):
			return argv[i+1:], true, ""
		case optionIs(f, "file", 1):
			return nil, false, "" // a script file: not inspectable, the wrapper ask stays
		case psNoValue[f]:
		case psTakesValue(f):
			i++ // skip the option's value
		default:
			return nil, true, "unknown PowerShell option " + a
		}
	}
	return nil, false, ""
}

// cmdPayload finds the words of cmd /c, /k or /r (case-insensitive; the
// command may be glued to the flag as in cmd /cdir). Everything after the flag
// is the command line.
func cmdPayload(argv []string) (parts []string, found bool, reason string) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if len(a) < 2 || a[0] != '/' {
			return nil, false, "" // cmd without /c: interactive, left to the wrapper ask
		}
		switch strings.ToLower(a)[1] {
		case 'c', 'k', 'r':
			var out []string
			if glued := a[2:]; glued != "" {
				out = append(out, glued)
			}
			out = append(out, argv[i+1:]...)
			for _, w := range out {
				if strings.ContainsAny(w, "\r\n") {
					// cmd /c ends at the first newline: what follows is not part of the command.
					return nil, true, "cmd command line contains a newline"
				}
			}
			return out, true, ""
		case 'v':
			// /v:on turns on delayed !VAR! expansion, which can build a command name.
			return nil, true, "cmd delayed expansion option " + a
		case 'q', 'd', 'a', 'u', 's', 'e', 'f', 't':
			// /q /d /a /u /s take no value; /e: /f: /t: carry theirs after a colon.
		default:
			return nil, true, "unknown cmd option " + a
		}
	}
	return nil, false, ""
}
