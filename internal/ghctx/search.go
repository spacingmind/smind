package ghctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// searchTimeout bounds a single Search (list, or view for a number/URL
// query).
const searchTimeout = 15 * time.Second

// ghItemJSON is gh's --json output shape for pr/issue list rows.
type ghItemJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	URL         string `json:"url"`
	UpdatedAt   string `json:"updatedAt"`
	HeadRefName string `json:"headRefName"`
	// IsCrossRepository is only requested for pr lists.
	IsCrossRepository bool `json:"isCrossRepository"`
}

// ghURLRe matches a github.com PR/issue URL with a strict owner/repo and
// number, so a URL query resolves to exactly that PR/issue -- kind taken
// from the path (pull -> pr, issues -> issue) regardless of the kind
// argument.
var ghURLRe = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100})/(pull|issues)/([0-9]+)$`)

// Search lists kind's ("pr"|"issue") open items in dir's repo, filtered
// by query when non-empty. A query that is a bare/#-prefixed number or a
// github.com pull/issues URL resolves that one item instead of listing:
// an empty Items (not an error) if gh says it doesn't exist.
func (c *Client) search(ctx context.Context, dir, kind, query string, limit int) (forge.SearchResult, error) {
	if kind != "pr" && kind != "issue" {
		return forge.SearchResult{}, fmt.Errorf("invalid arguments")
	}
	if limit <= 0 || limit > 30 {
		limit = 30
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	if q := strings.TrimSpace(query); q != "" {
		if n, k, repo, ok := resolveQuery(q, kind); ok {
			return c.searchOne(ctx, dir, k, n, repo)
		}
	}

	repo, err := c.repoOf(ctx, dir)
	if err != nil {
		return forge.SearchResult{}, err
	}
	fields := "number,title,state,author,url,updatedAt,headRefName,isCrossRepository"
	if kind == "issue" {
		fields = "number,title,state,author,url,updatedAt"
	}
	args := []string{kind, "list", "--state", "open", "--limit", strconv.Itoa(limit), "--json", fields}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "--search", q)
	}
	out, err := c.run(ctx, dir, args...)
	if err != nil {
		return forge.SearchResult{}, c.mapRunError(ctx, err)
	}
	var rows []ghItemJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return forge.SearchResult{}, fmt.Errorf("parse gh %s list output: %w", kind, err)
	}
	items := make([]forge.Item, len(rows))
	for i, r := range rows {
		items[i] = forge.Item{
			Number:    r.Number,
			Title:     r.Title,
			State:     strings.ToLower(r.State),
			Author:    r.Author.Login,
			URL:       r.URL,
			UpdatedAt: r.UpdatedAt,
			HeadRef:   r.HeadRefName,
			FromFork:  r.IsCrossRepository,
		}
	}
	return forge.SearchResult{Repo: repo, Items: items}, nil
}

// searchOne resolves a number/URL query to a one-item result. A gh
// not-found surfaces as empty Items (the attach UI's "nothing to show"),
// not an error.
func (c *Client) searchOne(ctx context.Context, dir, kind string, number int, repo string) (forge.SearchResult, error) {
	snap, err := c.Get(ctx, dir, kind, number, repo)
	if err != nil {
		var fe *forge.Error
		if errors.As(err, &fe) && fe.Code == forge.CodeNotFound {
			return forge.SearchResult{Repo: repo}, nil
		}
		return forge.SearchResult{}, err
	}
	item := forge.Item{
		Number:  snap.Number,
		Title:   snap.Title,
		State:   snap.State,
		URL:     snap.URL,
		HeadRef: snap.HeadRef,
	}
	return forge.SearchResult{Repo: snap.Repo, Items: []forge.Item{item}}, nil
}

// resolveQuery detects a bare-number ("123", "#123") or github.com
// pull/issues URL query. For a bare number the caller's kind is used with
// no explicit repo; for a URL, kind and repo come from the URL itself.
func resolveQuery(q, kind string) (number int, k string, repo string, ok bool) {
	if m := ghURLRe.FindStringSubmatch(q); m != nil {
		n, err := strconv.Atoi(m[4])
		if err != nil {
			return 0, "", "", false
		}
		k = "pr"
		if m[3] == "issues" {
			k = "issue"
		}
		return n, k, m[1] + "/" + m[2], true
	}
	nq := strings.TrimPrefix(q, "#")
	if n, err := strconv.Atoi(nq); err == nil && n > 0 {
		return n, kind, "", true
	}
	return 0, "", "", false
}
