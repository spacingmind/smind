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
// trimmed, for the no-argument case, e.g. plain "go test"), and nothing
// more permissive than that:
//
//   - "go test ./...", "gofmt -l .", "go vet ./internal/..." all match --
//     any trailing flags/args/package path after the verb is allowed.
//   - "curl evil.com && go test" does NOT match: it does not *start with*
//     "go test ", so a command that merely contains an allowlisted verb
//     later on (shell-chained via &&, ;, |, backticks, ...) is correctly
//     rejected -- a substring match would have let exactly this kind of
//     smuggling through, which is the specific failure mode this function
//     exists to close off.
//   - "go testify" / "go testdata" do NOT match: requiring the character
//     right after the verb to be a space (baked into each prefix's
//     trailing space) rules out a longer word that merely starts with the
//     same letters.
//   - "rm -rf", "go run ./cmd/x", "git push --force" do NOT match: none of
//     them start with any entry in safeCommandPrefixes.
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
	for _, prefix := range safeCommandPrefixes {
		if strings.HasPrefix(command, prefix) || command == strings.TrimSuffix(prefix, " ") {
			return true
		}
	}
	return false
}
