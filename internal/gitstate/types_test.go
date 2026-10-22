package gitstate

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func flattenKeys(prefix string, v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			*out = append(*out, p)
			// actions values are keyed by action name, not by schema.
			if k == "actions" {
				for _, av := range c.(map[string]any) {
					flattenKeys(p+".<name>", av, out)
				}
				continue
			}
			flattenKeys(p, c, out)
		}
	case []any:
		for _, c := range x {
			flattenKeys(prefix+"[]", c, out)
		}
	}
}

// The mobile-facing shape must not drift: this is the exact key set.
func TestTaskGit_FieldListPinned(t *testing.T) {
	ts := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	g := TaskGit{
		TaskID: 12, Forge: "github",
		Branch:   &Branch{Name: "b", Owned: true},
		Base:     &Base{Ref: "develop", Sha: "s", Tip: "t", Known: true},
		Diff:     &Diff{1, 2, 3},
		Worktree: &Worktree{true, 1, 2, 3},
		Commits:  &Commits{ip(1), ip(2), ip(3)},
		Origin:   &Origin{Upstream: "origin/b", Published: true, Ahead: ip(1), Behind: ip(2)},
		Conflict: &Conflict{State: "conflicts", Files: []string{"a"}, Against: "origin/develop", CheckedAt: ts, Reason: "r"},
		Merged:   &Merged{State: "merged", Via: "pr"},
		PR: &PRInfo{
			Number: 1, Repo: "o/r", URL: "u", Title: "t", State: "open", Draft: true, HeadRef: "h", HeadSha: "s",
			BaseRef: "b", FromFork: true, Mergeable: "mergeable", ReviewDecision: "approved",
			Checks:    &Checks{"success", 1, 2, 3, 6},
			FetchedAt: ts, Stale: "budget", RefreshAvailableAt: "x",
		},
		Actions:    map[string]Action{"createPr": {Enabled: false, Code: "c", Reason: "r"}},
		Untrusted:  true,
		ComputedAt: ts,
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	var got []string
	flattenKeys("", generic, &got)
	sort.Strings(got)

	want := []string{
		"actions", "actions.<name>.code", "actions.<name>.enabled", "actions.<name>.reason",
		"base", "base.known", "base.ref", "base.sha", "base.tip",
		"branch", "branch.name", "branch.owned",
		"commits", "commits.aheadOfBase", "commits.behindBase", "commits.sinceFork",
		"computedAt",
		"conflict", "conflict.against", "conflict.checkedAt", "conflict.files", "conflict.reason", "conflict.state",
		"diff", "diff.deletions", "diff.files", "diff.insertions",
		"forge",
		"merged", "merged.state", "merged.via",
		"origin", "origin.ahead", "origin.behind", "origin.published", "origin.upstream",
		"pr", "pr.baseRef", "pr.checks", "pr.checks.failed", "pr.checks.passed", "pr.checks.pending", "pr.checks.state", "pr.checks.total",
		"pr.draft", "pr.fetchedAt", "pr.fromFork", "pr.headRef", "pr.headSha", "pr.mergeable", "pr.number",
		"pr.refreshAvailableAt", "pr.repo", "pr.reviewDecision", "pr.stale", "pr.state", "pr.title", "pr.url",
		"taskId", "untrusted",
		"worktree", "worktree.dirty", "worktree.staged", "worktree.unstaged", "worktree.untracked",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("key set drifted.\n got: %v\nwant: %v", got, want)
	}
}

func TestTaskGit_AbsentObjectsOmitted(t *testing.T) {
	raw, _ := json.Marshal(TaskGit{TaskID: 1})
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"branch", "base", "diff", "worktree", "commits", "origin", "conflict", "merged", "pr", "actions"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q present on an empty TaskGit; absent must mean not known", k)
		}
	}
}
