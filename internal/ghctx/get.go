package ghctx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// repoRe bounds an explicit --repo argument: owner/repo of GitHub-legal
// characters, so a repo like "a/b;rm -rf" is rejected before any gh
// invocation (the runner is argv-only anyway, but rejecting early keeps
// the gh command from being a confusing gh-side failure).
var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// getTimeout bounds a single Get (snapshot fetch + possible repo
// inference).
const getTimeout = 20 * time.Second

// ghViewJSON is gh's --json output shape for pr/issue view, the subset of
// fields ghctx consumes. author is an object {login}; issues lack
// baseRefName/headRefName (absent in their --json output, so the fields
// just stay empty).
type ghViewJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	BaseRefName string `json:"baseRefName"`
	HeadRefName string `json:"headRefName"`
	UpdatedAt   string `json:"updatedAt"`
}

// Get fetches kind's ("pr"|"issue") snapshot for number, running gh in
// dir. repo "" infers the repo from dir via `gh repo view`; a non-empty
// repo is passed as --repo and must match repoRe.
func (c *Client) get(ctx context.Context, dir, kind string, number int, repo string) (*forge.Snapshot, error) {
	if number <= 0 || (repo != "" && !repoRe.MatchString(repo)) || (kind != "pr" && kind != "issue") {
		return nil, fmt.Errorf("invalid arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	args := []string{kind, "view", fmt.Sprint(number), "--json", "number,title,url,state,body"}
	if kind == "pr" {
		args[len(args)-1] = "number,title,url,state,body,baseRefName,headRefName"
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	} else {
		var err error
		repo, err = c.repoOf(ctx, dir)
		if err != nil {
			return nil, err
		}
	}
	out, err := c.run(ctx, dir, args...)
	if err != nil {
		return nil, c.mapRunError(ctx, err)
	}
	var v ghViewJSON
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, fmt.Errorf("parse gh %s view %d output: %w", kind, number, err)
	}
	return &forge.Snapshot{
		Number:  v.Number,
		Repo:    repo,
		Title:   v.Title,
		URL:     v.URL,
		State:   strings.ToLower(v.State),
		BaseRef: v.BaseRefName,
		HeadRef: v.HeadRefName,
		Body:    v.Body,
	}, nil
}

// repoOf resolves dir's GitHub repository ("owner/name"); see repoInfo.
func (c *Client) repoOf(ctx context.Context, dir string) (string, error) {
	r, err := c.repoInfo(ctx, dir)
	if err != nil {
		return "", err
	}
	return r.NameWithOwner, nil
}

// repoInfo resolves dir's GitHub repository via `gh repo view --json
// nameWithOwner,url` run in dir: "owner/name" plus the forge host taken from
// the repo URL ("" when gh gives no parsable URL). Cached per dir for 60s so
// a task's search + snapshot pair doesn't pay for it twice.
func (c *Client) repoInfo(ctx context.Context, dir string) (forge.Repo, error) {
	if r, ok := c.repos.get(dir); ok {
		return r.(forge.Repo), nil
	}
	out, err := c.run(ctx, dir, "repo", "view", "--json", "nameWithOwner,url")
	if err != nil {
		return forge.Repo{}, c.mapRunError(ctx, err)
	}
	var v struct {
		NameWithOwner string `json:"nameWithOwner"`
		URL           string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return forge.Repo{}, fmt.Errorf("parse gh repo view output: %w", err)
	}
	name := strings.TrimSpace(v.NameWithOwner)
	if name == "" {
		return forge.Repo{}, fmt.Errorf("gh repo view returned no nameWithOwner for %s", dir)
	}
	r := forge.Repo{NameWithOwner: name}
	if u, err := url.Parse(strings.TrimSpace(v.URL)); err == nil && u.Scheme == "https" {
		r.Host = strings.ToLower(u.Host)
	}
	c.repos.set(dir, r)
	return r, nil
}

// ttlNow is the clock the cache reads (the injected clock, if any).
func (c *Client) ttlNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
