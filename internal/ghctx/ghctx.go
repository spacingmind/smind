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
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// Runner executes a gh command with argv (never a shell) in dir, returning
// gh's stdout. The default runs the real gh binary with cmd.Dir = dir,
// stderr captured and clipped inside the returned error.
type Runner func(ctx context.Context, dir string, args ...string) (stdout string, err error)

// StdinRunner is Runner with a string fed to gh's stdin. Create PR passes the
// pull request body this way (`--body-file -`) so user text never reaches
// argv, the process list or a shell.
type StdinRunner func(ctx context.Context, dir, stdin string, args ...string) (stdout string, err error)

// stderrClip bounds how much of gh's stderr is carried inside an error
// string: enough to map error kinds and show a human, not enough to dump
// gh's whole diagnostic output into a run log.
const stderrClip = 500

// defaultRunner shells out to the gh CLI the same way internal/workspace's
// runGH does: argv only (no shell), cmd.Dir = dir, stderr captured. It is
// bounded by ctx via CommandContext so a hung gh cannot outlive its
// caller's deadline.
func defaultRunner(ctx context.Context, dir string, args ...string) (string, error) {
	return runGH(ctx, dir, nil, args...)
}

// defaultStdinRunner is defaultRunner with stdin.
func defaultStdinRunner(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	return runGH(ctx, dir, strings.NewReader(stdin), args...)
}

func runGH(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// No argv in the error: it can carry a user's search text, and
		// stderr is scrubbed of tokens before it is clipped and wrapped.
		return "", fmt.Errorf("gh: %w: %s", err, clip(forge.Scrub(stderr.String())))
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

// WithStdinRunner replaces the gh runner that takes stdin (tests inject a stub).
func WithStdinRunner(r StdinRunner) Option { return func(c *Client) { c.runIn = r } }

// WithTTL overrides the result cache TTL (default 10s).
func WithTTL(d time.Duration) Option { return func(c *Client) { c.ttl = d } }

// WithNow overrides the cache's clock.
func WithNow(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// Client is a gh CLI client with a small result cache and single-flight.
type Client struct {
	run     Runner
	runIn   StdinRunner
	ttl     time.Duration
	now     func() time.Time
	repos   *ttlCache
	known   *ttlCache // dir -> forge.Repo last returned by Repo, for OpenPRs/PR's URL check
	results *ttlCache
	flights inflight
}

// New returns a Client with the default runner and a 10s TTL.
func New(opts ...Option) *Client {
	c := &Client{
		run:   defaultRunner,
		runIn: defaultStdinRunner,
		ttl:   10 * time.Second,
		now:   time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	// Built after the options so WithNow and WithTTL apply to the caches.
	c.repos = newTTLCache(60*time.Second, func() time.Time { return c.now() })
	c.known = newTTLCache(knownRepoTTL, func() time.Time { return c.now() })
	c.results = newTTLCache(c.ttl, func() time.Time { return c.now() })
	return c
}

// Available reports whether the gh binary is on the daemon host's PATH.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

var _ forge.Provider = (*Client)(nil)

// Name is the forge name this client serves.
func (c *Client) Name() string { return "github" }

// rateLimitFallback is how long a rate-limited error asks callers to wait
// when gh gives no retry hint.
const rateLimitFallback = 10 * time.Minute

var (
	retryAfterRe = regexp.MustCompile(`(?i)retry[- ]after:?\s*(\d+)`)
	resetRe      = regexp.MustCompile(`(?i)reset[^0-9]{0,40}(\d{10})\b`)
)

// mapRunError classifies a runner error into a typed *forge.Error. Matching
// is on lowercase text because gh's stderr wording varies across versions
// and locales. Detail carries the scrubbed, clipped error text (never argv:
// the runner doesn't put argv in its errors).
func (c *Client) mapRunError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	low := strings.ToLower(text)
	var code string
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() != nil):
		code = forge.CodeTimeout
	case errors.Is(err, exec.ErrNotFound) || strings.Contains(low, "executable file not found"):
		code = forge.CodeCLIMissing
	case strings.Contains(low, "rate limit") || strings.Contains(low, "http 429") ||
		(strings.Contains(low, "http 403") && (strings.Contains(low, "secondary") || strings.Contains(low, "rate"))):
		code = forge.CodeRateLimited
	case strings.Contains(low, "gh auth login") || strings.Contains(low, "not logged in") || strings.Contains(low, "authentication") || strings.Contains(low, "http 401"):
		code = forge.CodeUnauthenticated
	case strings.Contains(low, "no git remotes") || strings.Contains(low, "none of the git remotes") || strings.Contains(low, "could not determine base repo") || strings.Contains(low, "not a git repository") || strings.Contains(low, "unsupported host"):
		code = forge.CodeNoRemote
	case strings.Contains(low, "could not resolve to") || strings.Contains(low, "not found") || strings.Contains(low, "http 404") || strings.Contains(low, "no pull requests match"):
		code = forge.CodeNotFound
	default:
		code = forge.CodeError
	}
	fe := forge.NewError(code, text)
	if code == forge.CodeRateLimited {
		fe.RetryAt = c.ttlNow().Add(rateLimitFallback)
		if m := retryAfterRe.FindStringSubmatch(text); m != nil {
			if n, perr := strconv.Atoi(m[1]); perr == nil && n > 0 {
				fe.RetryAt = c.ttlNow().Add(time.Duration(n) * time.Second)
			}
		} else if m := resetRe.FindStringSubmatch(text); m != nil {
			if n, perr := strconv.ParseInt(m[1], 10, 64); perr == nil {
				fe.RetryAt = time.Unix(n, 0)
			}
		}
	}
	return fe
}
