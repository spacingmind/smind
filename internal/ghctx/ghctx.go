// Package ghctx talks to the gh CLI to resolve GitHub PR/issue context
// for a task's worktree: searching open PRs/issues and fetching one
// PR/issue snapshot (ADR-0038 §4). Everything shells out to gh -- never
// the network directly -- with an injectable runner so tests (and any
// future transports) never need real credentials.
package ghctx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes a gh command with argv (never a shell) in dir, returning
// gh's stdout. The default runs the real gh binary with cmd.Dir = dir,
// stderr captured and clipped inside the returned error.
type Runner func(ctx context.Context, dir string, args ...string) (stdout string, err error)

// stderrClip bounds how much of gh's stderr is carried inside an error
// string: enough to map error kinds and show a human, not enough to dump
// gh's whole diagnostic output into a run log.
const stderrClip = 500

// defaultRunner shells out to the gh CLI the same way internal/workspace's
// runGH does: argv only (no shell), cmd.Dir = dir, stderr captured. It is
// bounded by ctx via CommandContext so a hung gh cannot outlive its
// caller's deadline.
func defaultRunner(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %v: %w: %s", args, err, clip(stderr.String()))
	}
	return stdout.String(), nil
}

// clip trims s to at most stderrClip bytes.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= stderrClip {
		return s
	}
	return s[:stderrClip] + "...(clipped)"
}

// Option configures a Client.
type Option func(*Client)

// WithRunner replaces the gh runner (tests inject a stub here).
func WithRunner(r Runner) Option { return func(c *Client) { c.run = r } }

// WithTTL overrides the result cache TTL (default 10s).
func WithTTL(d time.Duration) Option { return func(c *Client) { c.ttl = d } }

// WithNow overrides the cache's clock.
func WithNow(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// Client is a gh CLI client with a small result cache and single-flight.
type Client struct {
	run     Runner
	ttl     time.Duration
	now     func() time.Time
	repos   *ttlCache
	results *ttlCache
	flights inflight
}

// New returns a Client with the default runner and a 10s TTL.
func New(opts ...Option) *Client {
	c := &Client{
		run: defaultRunner,
		ttl: 10 * time.Second,
		now: time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	// Built after the options so WithNow and WithTTL apply to the caches.
	c.repos = newTTLCache(60*time.Second, func() time.Time { return c.now() })
	c.results = newTTLCache(c.ttl, func() time.Time { return c.now() })
	return c
}

// Available reports whether the gh binary is on the daemon host's PATH.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Error codes for the wire (wsapi picks Code up via RPCCode).
const (
	CodeUnavailable = "forge_unavailable"
	CodeNotFound    = "forge_not_found"
)

// Error is ghctx's typed error: a machine-readable Code plus a
// human-presentable Message. Code is CodeUnavailable (gh missing /
// logged out / no GitHub remote -- environmental, retrying won't help)
// or CodeNotFound (no such PR/issue).
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) RPCCode() string { return e.Code }

// mapRunError classifies a runner error into a typed *Error, or returns a
// plain wrapped error carrying gh's (clipped) stderr for anything the
// classification doesn't recognize. Matching is on lowercase text because
// gh's stderr wording varies across versions and locales.
func mapRunError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return &Error{Code: CodeUnavailable, Message: "gh is not installed on the daemon host"}
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "executable file not found"):
		return &Error{Code: CodeUnavailable, Message: "gh is not installed on the daemon host"}
	case strings.Contains(low, "gh auth login") || strings.Contains(low, "not logged in") || strings.Contains(low, "authentication"):
		return &Error{Code: CodeUnavailable, Message: "gh is not logged in on the daemon host (run gh auth login)"}
	case strings.Contains(low, "no git remotes") || strings.Contains(low, "none of the git remotes") || strings.Contains(low, "could not determine base repo") || strings.Contains(low, "not a git repository"):
		return &Error{Code: CodeUnavailable, Message: "this workspace has no GitHub remote"}
	case strings.Contains(low, "could not resolve to") || strings.Contains(low, "not found") || strings.Contains(low, "no pull requests match"):
		return &Error{Code: CodeNotFound, Message: "pull request or issue not found"}
	default:
		return fmt.Errorf("gh: %w", err)
	}
}
