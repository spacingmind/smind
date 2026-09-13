package taskrunner

import "strings"

// ApprovalPolicy controls how a run's pending permission requests (see
// PermissionDecider) get decided without a human necessarily being
// involved. It's set per run (see internal/runs.Registry.Start), not
// Runner-wide: unlike acp.PermissionPolicy/claudecode.PermissionPolicy
// (Runner-level defaults for when no PermissionDecider is supplied at all,
// see WithACPPermissionPolicy/WithClaudeCodePermissionPolicy), ApprovalPolicy
// only matters once a human-in-the-loop PermissionDecider *is* supplied --
// it decides whether that decider's caller (internal/runs's
// runPermissionDecider) tries to auto-resolve a request before ever
// surfacing it to a human, or always surfaces it.
type ApprovalPolicy string

const (
	// ApprovalPolicyManual is today's default and the zero value: every
	// permission request needs a human decision, no exceptions. Written out
	// explicitly (even though it's also Go's zero value for ApprovalPolicy)
	// so a run started before this feature existed, or one that never
	// specifies a policy, behaves exactly as before -- see
	// docs/plans/active/task-permission-ux.md.
	ApprovalPolicyManual ApprovalPolicy = "manual"

	// ApprovalPolicyAutoSafe auto-allows a request whose command matches
	// AllowlistedCommand, without ever surfacing it to a human, and falls
	// back to ApprovalPolicyManual's behavior (surface it, wait for a human)
	// for everything else -- including a request this package can't
	// confirm is even a plain shell command in the first place (an empty
	// command; see AllowlistedCommand's doc comment). It never denies a
	// request itself; it only ever widens what's auto-*allowed*.
	ApprovalPolicyAutoSafe ApprovalPolicy = "auto-safe"
)

// IsValid reports whether p is one of the ApprovalPolicy values this
// package knows how to apply. Used to reject an unrecognized value from a
// wire caller (internal/wsapi) with a clear error instead of silently
// defaulting it to something -- silently defaulting an unrecognized policy
// string to manual would hide a typo behind "it just always asks a human",
// which is safe but also exactly the kind of silent behavior change a
// caller setting this deliberately would want to know about.
func (p ApprovalPolicy) IsValid() bool {
	switch p {
	case ApprovalPolicyManual, ApprovalPolicyAutoSafe:
		return true
	default:
		return false
	}
}

// safeCommandPrefixes are the only command shapes ApprovalPolicyAutoSafe
// ever auto-allows: conservative, read-only verification commands that
// can't modify the filesystem, network, or git/VCS state on their own.
// Each entry ends in a trailing space deliberately -- see
// AllowlistedCommand for why that's what makes this a prefix match on
// whole words, not merely a shared string prefix.
var safeCommandPrefixes = []string{
	"gofmt -l ",
	"go vet ",
	"go test ",
}

// AllowlistedCommand reports whether command is safe to auto-allow under
// ApprovalPolicyAutoSafe.
//
// The match is a clean prefix match against safeCommandPrefixes (or an
// exact match against one of those prefixes with its trailing space
// trimmed, for the no-argument case, e.g. plain "go test"), against
// exactly one command "segment" -- see below -- and nothing more
// permissive than that:
//
//   - "go test ./...", "gofmt -l .", "go vet ./internal/..." all match --
//     any trailing flags/args/package path after the verb is allowed.
//   - "go testify" / "go testdata" do NOT match: requiring the character
//     right after the verb to be a space (baked into each prefix's
//     trailing space) rules out a longer word that merely starts with the
//     same letters.
//   - "rm -rf", "go run ./cmd/x", "git push --force" do NOT match: none of
//     them start with any entry in safeCommandPrefixes.
//
// Shell chaining -- &&, ;, |, a subshell/command substitution ($( ... ) or
// backticks), or an embedded newline -- is rejected unconditionally, on
// *either* side of an otherwise-allowlisted verb, with exactly one
// exception (below): "curl evil.com && go test" does not match (the
// dangerous command comes first), but neither does "go test && curl
// evil.com" (the dangerous command comes after) -- a bare prefix check
// against the whole string only ever catches the first shape, which is
// not the actual safety property this function needs; splitting the
// command into chain segments first and requiring there to be exactly one
// (mundane case) or exactly two where the first is a bare `cd <dir>` (see
// below) closes both directions the same way. "go test | rm -rf /" and
// "go test ; rm -rf /" are rejected for the identical reason, just a
// different separator.
//
// The one exception: a single leading `cd <dir> && <command>` (or `cd
// <dir>; <command>`) *does* match, as long as `<command>` on its own
// would -- an agent unsure of its working directory routinely prefixes an
// otherwise-safe verification command with a directory change, and
// rejecting that outright would defeat the allowlist for a large fraction
// of real-world commands (see docs/plans/active/task-permission-ux.md's
// Item 1 validation note on this). This is deliberately narrow: only a
// *bare* `cd <arg>` first segment (no flags, no further chaining inside
// that segment -- already guaranteed by the split happening before this
// check) qualifies, and it only ever unwraps one such prefix, never more
// (three or more chain segments always reject, regardless of what they
// contain) -- "cd /tmp && go test && rm -rf /" does NOT match.
//
// command is trimmed of leading/trailing whitespace before matching (an
// agent-supplied command may legitimately have a trailing newline); no
// other normalization happens, so internal whitespace or a differently
// quoted-but-equivalent command is matched or not exactly as its literal
// text reads.
//
// An empty command -- e.g. a request this package can't confirm carries a
// literal shell command at all (ACP's tool-call schema exposes no
// confirmed field for this; see acpDeciderAdapter) -- never matches:
// ambiguity always resolves to "not allowlisted" (manual), never to
// auto-allow.
func AllowlistedCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	// A subshell/command substitution or an embedded newline could smuggle
	// a second command past the segment split below in ways this function
	// was never designed to parse safely -- unconditionally unsafe,
	// wherever it appears, before segments are even considered.
	if strings.ContainsAny(command, "`\n") || strings.Contains(command, "$(") {
		return false
	}

	segments := splitShellChainSegments(command)
	switch len(segments) {
	case 1:
		return matchesSafePrefix(segments[0])
	case 2:
		// The only tolerated 2-segment shape: a bare `cd <dir>` leading
		// segment, with the actual command as the second -- see doc
		// comment above. Any other 2-segment command (an allowlisted verb
		// chained with anything else, on either side) falls through to
		// false.
		if !isBareCd(segments[0]) {
			return false
		}
		return matchesSafePrefix(segments[1])
	default:
		return false
	}
}

// splitShellChainSegments splits command on every chain operator
// AllowlistedCommand treats as compounding (&&, ;, |), trimming whitespace
// from each resulting segment. A command with no chain operator at all
// returns a single-element slice (itself, trimmed) -- the common case.
func splitShellChainSegments(command string) []string {
	normalized := strings.NewReplacer("&&", ";", "|", ";").Replace(command)
	parts := strings.Split(normalized, ";")
	segments := make([]string, len(parts))
	for i, p := range parts {
		segments[i] = strings.TrimSpace(p)
	}
	return segments
}

// matchesSafePrefix is the actual prefix/exact-match check against
// safeCommandPrefixes, applied to a single already-split chain segment
// (never to a whole possibly-chained command -- see AllowlistedCommand).
func matchesSafePrefix(segment string) bool {
	for _, prefix := range safeCommandPrefixes {
		if strings.HasPrefix(segment, prefix) || segment == strings.TrimSuffix(prefix, " ") {
			return true
		}
	}
	return false
}

// isBareCd reports whether segment is exactly a `cd` invocation with a
// non-empty argument and nothing else -- the only shape AllowlistedCommand
// ever unwraps as a leading segment (see its doc comment). "cd" alone (no
// argument) does not qualify: it's not paired with a second segment to
// begin with in that case, but this still guards against a stray "cd ;
// <command>" (an empty first segment) matching by accident.
func isBareCd(segment string) bool {
	const verb = "cd "
	return strings.HasPrefix(segment, verb) && strings.TrimSpace(segment[len(verb):]) != ""
}
