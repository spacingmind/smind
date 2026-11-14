package crash

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The path rules and the secret rules below implement the redaction contract
// of docs/plans/active/crash-capture.md (Decisions): no path under the user's
// directories and no known secret shape reaches disk. It is a defence in
// depth for the stack text and for runtime error messages (non-runtime panic
// messages are never stored at all); it cannot recognise a secret of an
// unknown shape.
var (
	// A user directory name may contain spaces ("C:\Users\Alice Smith"): take the
	// whole component, words separated by single spaces, up to the next
	// separator or any other character a name cannot hold.
	reUnixUserDir = regexp.MustCompile(`(?i)(?:/Users/|/home/)[\p{L}\p{N}_.'+@-]+(?: [\p{L}\p{N}_.'+@-]+)*|/root\b`)
	reWindowsUser = regexp.MustCompile(`(?i)[A-Za-z]:[\\/]Users[\\/][\p{L}\p{N}_.'+@-]+(?: [\p{L}\p{N}_.'+@-]+)*`)
	reBearer      = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)
	reSK          = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`)
	reGitHubToken = regexp.MustCompile(`\b(?:ghp|gho|ghs|ghu|ghr)_[A-Za-z0-9]{16,}`)
	reGitHubPat   = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{16,}`)
	reAWSKey      = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	reJWT         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)
	// key = value, key: value and "key":"value" for a key that names a secret;
	// the value is a quoted string (spaces allowed) or the rest of the word.
	reKeyValue = regexp.MustCompile(`(?i)(["']?[a-z0-9_-]*(?:token|secret|pass(?:word|wd|phrase)?|apikey|api[_-]key|authorization|cookie|credential|private[_-]?key)[a-z0-9_-]*["']?\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)`)
	// An Authorization or Cookie header carries a scheme word and then the
	// credential ("Token abc", "Digest ...", "sid=1; other=2"): take the rest of
	// the line, not just the first word.
	reAuthLine = regexp.MustCompile(`(?i)(["']?(?:proxy-)?authorization["']?\s*[=:]\s*|\b(?:set-)?cookie["']?\s*[=:]\s*)[^\r\n]*`)
	// A bare "<scheme> <credential>" for the schemes that name a credential type.
	reSchemeCred  = regexp.MustCompile(`(?i)(\b(?:token|apikey|api-key|digest|negotiate|ntlm)\s+)[A-Za-z0-9._~+/=-]{12,}`)
	reURLUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	reHexRun      = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reHighEntropy = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)
	rePEM         = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	// Vendor token shapes: Slack, Google, GitLab, npm, Google OAuth, Stripe.
	reVendorTokens = regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{8,}|\bAIza[0-9A-Za-z_-]{30,}|\bglpat-[A-Za-z0-9_-]{16,}|\bnpm_[A-Za-z0-9]{30,}|\bya29\.[A-Za-z0-9._-]{20,}|\b[sr]k_(?:live|test)_[A-Za-z0-9]{12,}`)
	rePlainSecrets = []*regexp.Regexp{rePEM, reBearer, reSK, reGitHubToken, reGitHubPat, reAWSKey, reVendorTokens, reJWT, reHexRun}
)

// Redact removes paths under the user's home and smind home, other users'
// directories, and secret shapes from s. It is applied to the message and
// the stack of every report before they are written.
func Redact(s, home, smindHome string) string {
	// Applied until nothing changes (at most 3 passes): one rule's output can
	// expose the input of another (a path inside a quoted value, a secret
	// next to a home), and the stored text must be stable under Redact.
	for i := 0; i < 3; i++ {
		next := redactOnce(s, home, smindHome)
		if next == s {
			break
		}
		s = next
	}
	return s
}

func redactOnce(s, home, smindHome string) string {
	s = redactPaths(s, home, smindHome)
	// The Windows shape runs first: its drive-letter prefix would otherwise
	// survive a /Users/... match cut in the middle of the path.
	s = reWindowsUser.ReplaceAllString(s, "<user dir>")
	s = reUnixUserDir.ReplaceAllString(s, "<user dir>")
	s = reAuthLine.ReplaceAllString(s, "${1}[redacted]")
	s = reSchemeCred.ReplaceAllString(s, "${1}[redacted]")
	for _, re := range rePlainSecrets {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	s = reKeyValue.ReplaceAllString(s, "${1}[redacted]")
	s = reURLUserinfo.ReplaceAllString(s, "${1}[redacted]@")
	s = reHighEntropy.ReplaceAllStringFunc(s, redactHighEntropyRun)
	// Defence in depth: a secret rule must not be able to split a path back
	// into visibility. If anything of the homes survived, replace it again.
	return redactPaths(s, home, smindHome)
}

// redactPaths replaces the smind home with $SMIND_HOME and the home with ~,
// longest path first (the smind home usually lives under the home, so it
// must be replaced before the home swallows it). Each path is matched in
// its given form, its slash form and its backslash form, so a Windows path
// is caught regardless of which separator the panicking code used.
func redactPaths(s, home, smindHome string) string {
	pairs := make([]struct{ path, repl string }, 0, 2)
	for _, p := range []struct{ path, repl string }{
		{smindHome, "$SMIND_HOME"},
		{home, "~"},
	} {
		p.path = strings.TrimRight(p.path, `/\`)
		if p.path == "" || p.path == "/" {
			continue
		}
		pairs = append(pairs, p)
	}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].path) > len(pairs[j].path) })
	for _, p := range pairs {
		forms := []string{p.path, filepath.ToSlash(p.path), strings.ReplaceAll(p.path, "/", `\`)}
		// The home as the OS resolves it can differ from the one in the
		// environment (/var -> /private/var, a symlinked home).
		if real := resolvedPath(p.path); real != "" && real != p.path {
			forms = append(forms, real, filepath.ToSlash(real))
		}
		for _, form := range forms {
			s = replaceAtBoundary(s, form, p.repl)
		}
	}
	return s
}

// resolvedCache remembers filepath.EvalSymlinks per path: redaction runs on
// every report and the lookup can be slow (network homes, automounts). A home
// that is re-pointed while the process runs is not followed; the generic
// user-directory rules still apply.
var resolvedCache sync.Map // string -> string ("" when the path does not resolve)

func resolvedPath(path string) string {
	if v, ok := resolvedCache.Load(path); ok {
		return v.(string)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = ""
	}
	resolvedCache.Store(path, real)
	return real
}

// replaceAtBoundary replaces path in s only where it ends at a path boundary:
// /Users/al must not eat the start of /Users/alice or /Users/alicebob (that
// would leave "~ice" and the neighbour's name).
func replaceAtBoundary(s, path, repl string) string {
	if path == "" {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, path)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(path)
		if end < len(s) && isPathNameByte(s[end]) {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s = s[end:]
	}
}

// isPathNameByte reports whether c would continue a path component name.
func isPathNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '-' || c == '.' || c == '%' || c == '~' || c >= 0x80
}

// redactHighEntropyRun replaces a 32+ character mixed-case alphanumeric run
// (at least one digit, one upper-case and one lower-case letter) with
// [redacted]; anything else is left alone.
func redactHighEntropyRun(run string) string {
	var digit, upper, lower bool
	for _, r := range run {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		}
	}
	if digit && upper && lower {
		return "[redacted]"
	}
	return run
}
