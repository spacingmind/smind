package ghctx

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/forge"
)

// CreatePR opens a pull request with `gh pr create --base <b> --head <h>
// --title <t> [--draft] --body-file -` run in dir, the body on stdin (never
// argv). The PR URL gh prints is validated against the repo's host before it
// is trusted; the full PR state then comes from one `gh pr view`. Both calls
// are bounded by their own 20 s deadline.
func (c *Client) CreatePR(ctx context.Context, dir string, in forge.CreatePRInput) (forge.PR, error) {
	if in.Base == "" || in.Head == "" || in.Title == "" ||
		strings.HasPrefix(in.Base, "-") || strings.HasPrefix(in.Head, "-") {
		return forge.PR{}, forge.NewError(forge.CodeError, "invalid pull request arguments")
	}
	number, err := c.createPR(ctx, dir, in)
	if err != nil {
		return forge.PR{}, err
	}
	return c.PR(ctx, dir, number)
}

// createPR runs the create call and returns the new PR's number.
func (c *Client) createPR(ctx context.Context, dir string, in forge.CreatePRInput) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	repo, err := c.knownRepo(ctx, dir)
	if err != nil {
		return 0, err
	}
	args := []string{"pr", "create", "--base", in.Base, "--head", in.Head, "--title", in.Title}
	if in.Draft {
		args = append(args, "--draft")
	}
	args = append(args, "--body-file", "-")
	out, err := c.runIn(ctx, dir, in.Body, args...)
	if err != nil {
		return 0, c.mapRunError(ctx, err)
	}
	// gh prints the URL as its last line (earlier lines can be warnings).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	raw := strings.TrimSpace(lines[len(lines)-1])
	canon, ok := forge.ValidPRURL(raw, repo.Host)
	if !ok {
		return 0, forge.NewError(forge.CodeError, "gh pr create printed no usable pull request url")
	}
	u, err := url.Parse(canon)
	if err != nil {
		return 0, forge.NewError(forge.CodeError, "gh pr create printed no usable pull request url")
	}
	n, err := strconv.Atoi(u.Path[strings.LastIndex(u.Path, "/")+1:])
	if err != nil || n <= 0 {
		return 0, forge.NewError(forge.CodeError, "gh pr create printed no usable pull request url")
	}
	return n, nil
}
