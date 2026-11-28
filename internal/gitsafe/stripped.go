// Stripped mode: the configuration git runs with when the daemon started
// the call itself (P0 "daemon-initiated git runs stripped"). The human mode
// (Args/Env) keeps what the repository names working; this mode assumes an
// agent may have written the repository's git state (.git/config,
// .git/hooks, .git/info/attributes, an in-tree .gitattributes) and
// neutralises every program that state can make git run: hooks, filters,
// merge drivers, credential helpers, askpass, ssh commands, signing
// programs and upload/receive-pack programs.
package gitsafe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// refOnlySubcommands is the allow-list of subcommands that only read refs
// or the configuration and can run none of the classes this mode
// neutralises: no filter, merge driver, hook, helper or program. The scan
// is skipped for exactly these. An unknown subcommand, no subcommand at all
// and every subcommand not listed here is scanned, so a new subcommand
// fails safe (it is scanned even though it might not need to be).
//
// cat-file is deliberately absent (--filters and --textconv run a filter),
// and so is remote as a plain member: remote update, remote add -f and
// remote prune do network work that runs uploadpack, credential and ssh
// programs, so only remote's read-only shapes qualify, which refOnlyCall
// decides. tag is absent too: `tag -s` signs through gpg.program, which
// commit.gpgsign=false does not stop.
var refOnlySubcommands = map[string]bool{
	"rev-parse":        true,
	"rev-list":         true,
	"for-each-ref":     true,
	"symbolic-ref":     true,
	"merge-base":       true,
	"config":           true,
	"branch":           true,
	"show-ref":         true,
	"ls-tree":          true,
	"name-rev":         true,
	"describe":         true,
	"check-ref-format": true,
}

// refOnlyCall reports whether the call args[sub] is the subcommand of only
// reads refs or the configuration: a member of refOnlySubcommands, or the
// subcommand remote with nothing but a read-only shape after it (up to any
// "--"): no arguments, exactly -v or --verbose, get-url ..., or
// show -n .... Every other remote call (update, add -f, prune, set-head)
// is scanned, because it can run a program.
func refOnlyCall(args []string, sub int) bool {
	if sub < 0 || sub >= len(args) {
		return false
	}
	if refOnlySubcommands[args[sub]] {
		return true
	}
	if args[sub] != "remote" {
		return false
	}
	rest := beforePathspecs(args[sub+1:])
	if len(rest) == 0 {
		return true
	}
	if len(rest) == 1 && (rest[0] == "-v" || rest[0] == "--verbose") {
		return true
	}
	if rest[0] == "get-url" {
		return true
	}
	return len(rest) >= 2 && rest[0] == "show" && rest[1] == "-n"
}

// scanConfig reads the configuration the overrides are derived from, as a
// package variable so the tests can drive the parser without a repository.
// The read itself runs no program (a config read executes nothing).
var scanConfig = defaultScan

// scanTimeout bounds the config read: a hang must fail closed, not hold
// the daemon.
const scanTimeout = 5 * time.Second

// configEntry is one entry of the scanned configuration, as git printed
// it: scope and origin decide trust, key and value decide the override.
type configEntry struct {
	scope  string
	origin string
	key    string
	value  string
}

// StrippedEnv returns the baseline environment (Env(base)): stripped mode
// adds no environment change of its own.
func StrippedEnv(base []string) []string {
	return Env(base)
}

// StrippedArgs returns args for a git call the DAEMON started, with the
// stripped-mode overrides, as a new slice; dir is the directory git will
// run in (the repository or worktree; "" = the process working directory).
// The input slice is never modified.
//
// The result is the baseline Args(args...) — core.fsmonitor off,
// --no-ext-diff/--no-textconv for the diff family — with the static
// stripped overrides (hooks silenced, signing off) and, unless the
// subcommand only reads refs or the configuration, one scanned set of
// -c key=value pairs reasserting or emptying every program the
// repository's own configuration names. fetch, ls-remote and push also
// carry the flag that forces the default pack program, which no -c
// override can do. It returns an error, and the
// caller must not run git, when the config read fails: fail closed, never
// a call without the overrides. Config values are never logged.
func StrippedArgs(ctx context.Context, dir string, args ...string) ([]string, error) {
	base := Args(args...)
	extras := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "commit.gpgsign=false",
		"-c", "tag.gpgsign=false",
	}
	overrides, err := strippedOverrides(ctx, dir, args)
	if err != nil {
		return nil, err
	}
	extras = append(extras, overrides...)
	out := insertExtras(base, extras)
	return insertPackFlag(args, out), nil
}

// insertPackFlag returns out with the pack-program flag (packProgramFlag)
// for its subcommand inserted immediately after that subcommand, so it
// lands before every pathspec and after the -c extras, which sit before
// the subcommand: the subcommand is therefore found on the final slice.
// The skip search uses the caller's arguments, so a value one of this
// function's own -c pairs carries can never suppress the flag.
func insertPackFlag(args, out []string) []string {
	sub := subcommandIndex(out)
	if sub < 0 {
		return out
	}
	flag := packProgramFlag(out[sub], beforePathspecs(args))
	if flag == "" {
		return out
	}
	argv := make([]string, 0, len(out)+1)
	argv = append(argv, out[:sub+1]...)
	argv = append(argv, flag)
	return append(argv, out[sub+1:]...)
}

// packProgramFlag returns the command-line flag that forces the default
// pack program for subcommand, or "" when it needs none. A config
// override cannot do this: git collects remote.<name>.uploadpack (and
// receivepack) across scopes and uses the repository's value even when a
// command-line -c names another ("more than one uploadpack given, using
// the first", measured on git 2.54), while the command-line flag wins.
// The flag is skipped when searched — the arguments before any bare "--"
// — already carries the option in any of its shapes.
func packProgramFlag(subcommand string, searched []string) string {
	switch subcommand {
	case "fetch", "ls-remote":
		if hasFlag(searched, "--upload-pack") || hasOptionPrefix(searched, "--upload-pack=") || hasFlag(searched, "-u") {
			return ""
		}
		return "--upload-pack=git-upload-pack"
	case "push":
		if hasFlag(searched, "--receive-pack") || hasOptionPrefix(searched, "--receive-pack=") || hasFlag(searched, "--exec") {
			return ""
		}
		return "--receive-pack=git-receive-pack"
	}
	return ""
}

// hasOptionPrefix reports whether any element of args is opt with a value
// attached, as in --opt=value.
func hasOptionPrefix(args []string, opt string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, opt) {
			return true
		}
	}
	return false
}

// insertExtras returns a copy of base with extras inserted right after the
// leading "-c core.fsmonitor=false" pair (index 2 in the ordinary case),
// so every -c the stripped mode adds stays a global option, before the
// subcommand. A caller that carried the pair somewhere else before the
// subcommand still gets the extras after it; one that carried it only
// after the subcommand (where git would not accept a -c) gets them at the
// front instead. base is never modified.
func insertExtras(base, extras []string) []string {
	region := base
	if sub := subcommandIndex(base); sub >= 0 {
		region = base[:sub]
	}
	region = beforePathspecs(region)
	at := 0
	for i := 0; i+1 < len(region); i++ {
		if region[i] == "-c" && region[i+1] == "core.fsmonitor=false" {
			at = i + 2
			break
		}
	}
	out := make([]string, 0, len(base)+len(extras))
	out = append(out, base[:at]...)
	out = append(out, extras...)
	return append(out, base[at:]...)
}

// strippedOverrides returns the scanned "-c" pairs for the call, or none
// when the subcommand only reads refs or the configuration (it can run
// none of the neutralised classes) or the scan found no entries. An error
// means the caller must not run git.
func strippedOverrides(ctx context.Context, dir string, args []string) ([]string, error) {
	if refOnlyCall(args, subcommandIndex(args)) {
		return nil, nil
	}
	out, err := scanConfig(ctx, dir)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil // no match: nothing to override
	}
	entries, err := parseConfigRecords(out)
	if err != nil {
		return nil, err
	}
	return overridePairs(entries, dir)
}

// defaultScan is the one git run behind the overrides: a config read that
// runs no program, under the baseline hardening (Args), the baseline
// environment (Env) and a 5 s timeout. It returns stdout. Exit status 1
// with empty stdout is "no match" (nil, nil); every other failure — git
// missing, another exit status, a timeout, a cancelled context — is an
// error, so the caller fails closed.
func defaultScan(ctx context.Context, dir string) ([]byte, error) {
	args := []string{
		"config", "--show-scope", "--show-origin", "--null", "--get-regexp",
		`^(filter|merge|remote|credential|core|gpg)\.`,
	}
	if dir != "" && filepath.IsAbs(dir) {
		args = append([]string{"-C", dir}, args...)
	}
	sctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cmd := exec.CommandContext(sctx, "git", Args(args...)...)
	cmd.Env = Env(nil)
	if dir != "" && !filepath.IsAbs(dir) {
		// A relative -C would be resolved by git against a directory it
		// was itself told to change to; cmd.Dir resolves it once, against
		// this process's working directory, which is what dir means.
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && out.Len() == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("gitsafe: stripped-mode config scan: %w", err)
	}
	return out.Bytes(), nil
}

// parseConfigRecords parses the --null --show-scope --show-origin output
// of the scan: each record is three NUL-terminated fields, the scope, the
// origin (like "file:.git/config" or "file:/abs/path") and "key\nvalue"
// (the key, a newline, the value; a key with no newline has the empty
// value). Entries keep git's order, so the last entry for a key is the one
// git honours. Anything that does not fit that shape is malformed and
// fails closed.
func parseConfigRecords(out []byte) ([]configEntry, error) {
	var entries []configEntry
	for len(out) > 0 {
		var fields [3][]byte
		for i := range fields {
			end := bytes.IndexByte(out, 0)
			if end < 0 {
				return nil, fmt.Errorf("gitsafe: malformed config record: %d complete fields, then end of output", i)
			}
			fields[i] = out[:end]
			out = out[end+1:]
		}
		key, value, _ := strings.Cut(string(fields[2]), "\n")
		entries = append(entries, configEntry{
			scope:  string(fields[0]),
			origin: string(fields[1]),
			key:    key,
			value:  value,
		})
	}
	return entries, nil
}

// trusted reports whether an entry is the person's own rather than the
// repository's: scope system, global or command, or scope unknown with a
// "file:" origin naming an absolute path outside dir (after Clean, with a
// path-separator-safe prefix test) that has no ".git" component — the
// shape Apple's Xcode system file reports. local and worktree are the
// repository's own (agent-editable) and untrusted, as is anything else.
func (e configEntry) trusted(dir string) bool {
	switch e.scope {
	case "system", "global", "command":
		return true
	case "unknown":
		path, ok := strings.CutPrefix(e.origin, "file:")
		if !ok || !filepath.IsAbs(path) {
			return false
		}
		clean := filepath.Clean(path)
		if hasDotGitComponent(clean) {
			return false
		}
		return !pathUnder(dir, clean)
	default:
		return false
	}
}

// pathUnder reports whether path is dir itself or inside it, both after
// Clean, with a separator-safe prefix test so "/base/repo" never counts
// "/base/repository" as inside.
func pathUnder(dir, path string) bool {
	d := filepath.Clean(dir)
	p := filepath.Clean(path)
	if d == string(filepath.Separator) {
		return strings.HasPrefix(p, string(filepath.Separator))
	}
	return p == d || strings.HasPrefix(p, d+string(filepath.Separator))
}

// hasDotGitComponent reports whether path has a ".git" path component.
func hasDotGitComponent(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	return false
}

// overridePairs turns the scanned entries into the "-c" "key=value"
// argument pairs that neutralise the repository-controlled programs. The
// pairs are sorted by key (the key exactly as git printed it: git
// lowercases the section and variable parts, so a plain compare is right)
// and the sort is stable, so the credential reset keeps its re-added
// helpers in their original order.
//
// For every filter, merge driver, ssh command, askpass, git proxy and
// signing-program key whose LAST entry is untrusted an override is
// emitted:
// filters and merge drivers get the last trusted value of the same key
// (so a local entry cannot hide the person's own "git lfs install"), the
// rest get the neutral value that turns the program off. Credentials are
// handled as a list, because helpers accumulate: one untrusted helper
// anywhere resets the list and re-adds only the trusted ones.
func overridePairs(entries []configEntry, dir string) ([]string, error) {
	type pair struct{ key, value string }
	var pairs []pair

	last := make(map[string]configEntry, len(entries))
	for _, e := range entries {
		last[e.key] = e // git's order: the last entry for a key wins
	}
	keys := make([]string, 0, len(last))
	for k := range last {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := last[k]
		if e.trusted(dir) {
			continue
		}
		if wouldOverride(k) && strings.Contains(k, "=") {
			return nil, errKeyHasEquals
		}
		switch {
		case isFilterKey(k, ".clean"), isFilterKey(k, ".smudge"), isFilterKey(k, ".process"):
			if e.value == lfsCommandLine(k) {
				continue // git-lfs's own command lines stay, even from a local scope
			}
			pairs = append(pairs, pair{k, lastTrustedValue(entries, k, dir)})
		case isNamedKey(k, "merge.", ".driver"):
			pairs = append(pairs, pair{k, lastTrustedValue(entries, k, dir)})
		case k == "core.sshcommand":
			pairs = append(pairs, pair{k, "ssh"})
		case k == "core.askpass", k == "core.gitproxy":
			pairs = append(pairs, pair{k, ""})
		case k == "gpg.program", k == "gpg.openpgp.program":
			pairs = append(pairs, pair{k, "gpg"})
		case k == "gpg.x509.program":
			pairs = append(pairs, pair{k, "gpgsm"})
		case k == "gpg.ssh.program":
			pairs = append(pairs, pair{k, "ssh-keygen"})
		}
	}

	for _, e := range entries {
		if isCredentialHelperKey(e.key) && !e.trusted(dir) && strings.Contains(e.key, "=") {
			return nil, errKeyHasEquals
		}
	}
	if untrustedHelper := hasUntrustedHelper(entries, dir); untrustedHelper {
		pairs = append(pairs, pair{"credential.helper", ""}) // the reset
		for _, e := range entries {
			if e.key == "credential.helper" && e.trusted(dir) {
				pairs = append(pairs, pair{"credential.helper", e.value})
			}
		}
	}

	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	out := make([]string, 0, 2*len(pairs))
	for _, p := range pairs {
		if strings.ContainsAny(p.key, "\n\x00") || strings.ContainsAny(p.value, "\n\x00") {
			return nil, errors.New("gitsafe: a repository config key or value contains a newline or NUL and cannot be neutralised")
		}
		out = append(out, "-c", p.key+"="+p.value)
	}
	return out, nil
}

// errKeyHasEquals is the fail-closed error for a key `-c key=value` cannot
// carry: git splits at the first '=', so such a key is never neutralised.
// It names no config value.
var errKeyHasEquals = errors.New("gitsafe: a repository config key contains '=' and cannot be neutralised")

// wouldOverride reports whether an untrusted entry for key gets an override.
func wouldOverride(k string) bool {
	switch {
	case isFilterKey(k, ".clean"), isFilterKey(k, ".smudge"), isFilterKey(k, ".process"),
		isNamedKey(k, "merge.", ".driver"):
		return true
	}
	switch k {
	case "core.sshcommand", "core.askpass", "core.gitproxy", "gpg.program",
		"gpg.openpgp.program", "gpg.x509.program", "gpg.ssh.program":
		return true
	}
	return false
}

// lastTrustedValue returns the value of the last trusted entry for key,
// or "" when there is none: what a neutralised key falls back to.
func lastTrustedValue(entries []configEntry, key, dir string) string {
	value := ""
	for _, e := range entries {
		if e.key == key && e.trusted(dir) {
			value = e.value
		}
	}
	return value
}

// hasUntrustedHelper reports whether any credential helper entry — plain
// credential.helper or a credential.<url>.helper — is untrusted: a local
// helper anywhere in the list is armed even when a later entry is trusted,
// because git runs every helper in the list.
func hasUntrustedHelper(entries []configEntry, dir string) bool {
	for _, e := range entries {
		if isCredentialHelperKey(e.key) && !e.trusted(dir) {
			return true
		}
	}
	return false
}

// isFilterKey reports whether key is a filter.<name> key of the given kind
// (.clean, .smudge or .process), the three that name a program.
func isFilterKey(key, kind string) bool {
	return isNamedKey(key, "filter.", kind)
}

// isNamedKey reports whether key has the given section prefix and variable
// suffix (both already lowercase, as git prints them).
func isNamedKey(key, section, variable string) bool {
	return strings.HasPrefix(key, section) && strings.HasSuffix(key, variable)
}

// isCredentialHelperKey reports whether key is credential.helper or a
// credential.<url>.helper.
func isCredentialHelperKey(key string) bool {
	return isNamedKey(key, "credential.", ".helper")
}

// lfsCommandLine returns the exact command line git-lfs writes for a
// filter key of the given kind, or "" when the key names none: the three
// lines are trusted even from a local scope ("git lfs install --local").
func lfsCommandLine(key string) string {
	switch {
	case strings.HasSuffix(key, ".clean"):
		return "git-lfs clean -- %f"
	case strings.HasSuffix(key, ".smudge"):
		return "git-lfs smudge -- %f"
	case strings.HasSuffix(key, ".process"):
		return "git-lfs filter-process"
	}
	return ""
}
