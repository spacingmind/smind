package crash

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// redactHomeRE, redactUserRE and the secret rules implement rule 2 of the
// crash-capture plan: no path under the user's directories and no secret
// shape reaches disk.
var (
	reUnixUserDir  = regexp.MustCompile(`(?i)(?:/Users/|/home/)[^/\s]+`)
	reWindowsUser  = regexp.MustCompile(`(?i)[A-Za-z]:[\\/]Users[\\/][^\\/\s]+`)
	reBearer       = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	reSK           = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`)
	reGitHubToken  = regexp.MustCompile(`\b(?:ghp|gho|ghs|ghu|ghr)_[A-Za-z0-9]{16,}`)
	reGitHubPat    = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{16,}`)
	reAWSKey       = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	reJWT          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)
	reKeyValue     = regexp.MustCompile(`(?i)\b([a-z0-9_-]*(?:token|secret|password|passwd|apikey|api_key|authorization)[a-z0-9_-]*\s*[=:]\s*)\S+`)
	reURLUserinfo  = regexp.MustCompile(`(?i)(https?://)[^/\s:@]+:[^/\s@]+@`)
	reHexRun       = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reHighEntropy  = regexp.MustCompile(`[A-Za-z0-9_-]{40,}`)
	rePlainSecrets = []*regexp.Regexp{reBearer, reSK, reGitHubToken, reGitHubPat, reAWSKey, reJWT, reHexRun}
)

// Redact removes paths under the user's home and smind home, other users'
// directories, and secret shapes from s. It is applied to the message and
// the stack of every report before they are written.
func Redact(s, home, smindHome string) string {
	s = redactPaths(s, home, smindHome)
	// The Windows shape runs first: its drive-letter prefix would otherwise
	// survive a /Users/... match cut in the middle of the path.
	s = reWindowsUser.ReplaceAllString(s, "<user dir>")
	s = reUnixUserDir.ReplaceAllString(s, "<user dir>")
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
		for _, form := range [3]string{p.path, filepath.ToSlash(p.path), strings.ReplaceAll(p.path, "/", `\`)} {
			s = strings.ReplaceAll(s, form, p.repl)
		}
	}
	return s
}

// redactHighEntropyRun replaces a 40+ character mixed-case alphanumeric run
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
