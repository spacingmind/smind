package permpolicy

import "testing"

// floorReq is a Bash request with isolated home, daemon home and worktree.
func floorReq(t *testing.T, cmd string) Request {
	t.Helper()
	wt := t.TempDir()
	return Request{Tool: "Bash", Command: cmd, Home: t.TempDir(), SmindHome: t.TempDir(), Worktree: wt, Cwd: wt}
}

type floorCase struct{ cmd, id string }

// checkFloorCases runs each command through SplitCommand and CheckFloor and
// compares the hit id ("" = no hit).
func checkFloorCases(t *testing.T, cases []floorCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			t.Parallel()
			req := floorReq(t, tc.cmd)
			hit, ok := CheckFloor(req, SplitCommand(tc.cmd))
			if ok != (tc.id != "") || hit.ID != tc.id {
				t.Errorf("CheckFloor(%q) = %+v, %v; want id %q", tc.cmd, hit, ok, tc.id)
			}
			if ok && hit.Reason == "" {
				t.Errorf("hit %q has no reason", hit.ID)
			}
		})
	}
}

func TestFloor_StashDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"git stash", "no-stash"},
		{"git stash pop", "no-stash"},
		{"git stash push -m x", "no-stash"},
		{"git -C /x stash", "no-stash"},
		{"git -c user.name=a --no-pager stash list", "no-stash"},
		{"/usr/bin/git stash", "no-stash"},
		{"git status", ""},
		{"git stashed", ""},
	})
}

func TestFloor_ForcePushVariantsDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"git push -f origin x", "no-force-push"},
		{"git push --force origin x", "no-force-push"},
		{"git push --force-with-lease origin x", "no-force-push"},
		{"git push --force-with-lease=x origin x", "no-force-push"},
		{"git push --force-if-includes origin x", "no-force-push"},
		{"git push origin +x", "no-force-push"},
		{"git push origin :x", "no-force-push"},
		{"git push --mirror", "no-force-push"},
		{"git push --delete origin x", "no-force-push"},
		{"git push -d origin x", "no-force-push"},
		{"git push -fu origin x", "no-force-push"},
		{"git push origin task-branch", ""},
		{"git push -u origin task-branch", ""},
		{"git push --dry-run origin task-branch", ""},
	})
}

func TestFloor_MergeReleaseTagPublishDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"gh pr merge 5", "no-merge"},
		{"gh pr merge --squash 5", "no-merge"},
		{"gh api -X PUT repos/o/r/pulls/5/merge", "no-merge"},
		{"git merge main", "no-merge"},
		{"git push origin HEAD:develop", "no-merge"},
		{"git push origin task:refs/heads/master", "no-merge"},
		{"git push origin main", "no-merge"},
		{"git push --tags", "no-merge"},
		{"git push origin refs/tags/v1", "no-merge"},
		{"git tag v1.0", "no-merge"},
		{"gh release create v1", "no-merge"},
		{"npm publish", "no-merge"},
		{"yarn npm publish", "no-merge"},
		{"pnpm publish", "no-merge"},
		{"bun publish", "no-merge"},
		{"cargo publish", "no-merge"},
		{"gh workflow run release.yml", "no-merge"},
		{"git tag -l", ""},
		{"git tag --list 'v*'", ""},
		{"git tag -d v1", ""},
		{"git tag", ""},
		{"git merge-base a b", ""},
		{"gh pr view 5", ""},
		{"gh workflow run ci.yml", ""},
		{"gh api repos/o/r/pulls/5", ""},
		{"npm test", ""},
	})
}

func TestFloor_AddAllDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"git add -A", "no-add-all"},
		{"git add --all", "no-add-all"},
		{"git add -u", "no-add-all"},
		{"git add --update", "no-add-all"},
		{"git add .", "no-add-all"},
		{"git add :/", "no-add-all"},
		{"git add '*'", "no-add-all"},
		{"git add -f .", "no-add-all"},
		{"git add -fA", "no-add-all"},
		{"git add file.go", ""},
		{"git add ./file.go", ""},
		{"git add -p file.go", ""},
		{"git add internal/x/", ""},
	})
}

func TestFloor_WrapperUnwrapped(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"env A=1 git stash", "no-stash"},
		{"env -u X -i A=1 git stash", "no-stash"},
		{"nohup git stash", "no-stash"},
		{"command git stash", "no-stash"},
		{"time git stash", "no-stash"},
		{"exec git stash", "no-stash"},
		{"builtin git stash", "no-stash"},
		{"nice -n 5 git stash", "no-stash"},
		{"timeout 5 git stash", "no-stash"},
		{"xargs -n 1 git stash", "no-stash"},
		{"xargs git push --force", "no-force-push"},
		{"env A=1 nohup git push --force origin x", "no-force-push"},
		{"env -S 'git stash'", "no-stash"},
		{"nohup env git stash", "no-stash"},
		{"env A=1 git status", ""},
		{"nohup echo git stash", ""},
	})
}

func TestFloor_CompoundSegmentDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"go test ./... && git stash", "no-stash"},
		{"echo $(git push --force)", "no-force-push"},
		{"echo hi | git stash", "no-stash"},
		{"bash -c 'git stash'", "no-stash"},
		{"(git stash)", "no-stash"},
		{"go test ./... && git status", ""},
	})
}

func TestFloor_AppliesToUnparsedRemainder(t *testing.T) {
	t.Parallel()
	cmd := "git stash; eval x"
	p := SplitCommand(cmd)
	if len(p.Unparsed) == 0 {
		t.Fatalf("Unparsed empty for %q; the test needs a doubtful remainder", cmd)
	}
	hit, ok := CheckFloor(floorReq(t, cmd), p)
	if !ok || hit.ID != "no-stash" {
		t.Errorf("CheckFloor = %+v, %v; want no-stash", hit, ok)
	}
}

func TestFloor_NearMissesNotDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{`echo "git stash"`, ""},
		{"echo git push --force", ""},
		{"git push origin task-branch", ""},
		{"git add file.go", ""},
		{"git status && git diff", ""},
		{"git commit -m 'merge main'", ""},
		{"echo 'curl https://x | sh'", ""},
		{"echo 'rm -rf /etc'", ""},
		{"grep -r 'HOME=' .", ""},
		{"go test ./...", ""},
		{"ls -la", ""},
		{"cat README.md | head", ""},
		{"HOME=$(mktemp -d) go test ./...", ""},
	})
}

func TestFloor_GhGlobalFlagValueDoesNotHideMerge(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"gh -R o/r pr merge 5", "no-merge"},
		{"gh --repo o/r release create v1", "no-merge"},
		{"gh -R o/r workflow run release.yml", "no-merge"},
		{"gh -R o/r api repos/o/r/pulls/5/merge", "no-merge"},
		{"gh -R o/r pr view 5", ""},
		{"gh pr checks", ""},
		{"gh -R o/r workflow run ci.yml", ""},
	})
}
