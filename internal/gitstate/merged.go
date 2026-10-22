package gitstate

import (
	"context"
	"strings"
)

// ComputeMerged decides whether the task's branch has landed. cmpRef is the
// full compare ref ("" when the base is not resolvable). sinceFork is the
// branch's commit count past base_sha: an empty branch is trivially an
// ancestor of its base and must not read as merged, and nil (base unknown)
// cannot tell the two apart, so the ancestor test is then inconclusive.
func ComputeMerged(ctx context.Context, run Runner, repoDir, cmpRef, branchRef string, sinceFork *int, pr *PRInfo) Merged {
	branchTip := revParse(ctx, run, repoDir, branchRef)
	if branchTip == "" {
		return Merged{State: MergedUnknown}
	}
	if cmpRef != "" {
		_, err := run(ctx, repoDir, "merge-base", "--is-ancestor", branchTip, cmpRef)
		switch {
		case err == nil:
			if sinceFork == nil {
				break // cannot rule out an empty branch
			}
			if *sinceFork > 0 {
				return Merged{State: MergedMerged, Via: MergedViaAncestor}
			}
			return Merged{State: MergedUnmerged}
		case exitCode(err) == 1:
			// not an ancestor: fall through to the PR test
		default:
			return mergedByPR(branchTip, pr, Merged{State: MergedUnknown})
		}
	}
	if pr != nil && prMergedAt(pr, branchTip) {
		return Merged{State: MergedMerged, Via: MergedViaPR}
	}
	if cmpRef == "" || sinceFork == nil {
		return Merged{State: MergedUnknown}
	}
	return Merged{State: MergedUnmerged}
}

func mergedByPR(branchTip string, pr *PRInfo, fallback Merged) Merged {
	if pr != nil && prMergedAt(pr, branchTip) {
		return Merged{State: MergedMerged, Via: MergedViaPR}
	}
	return fallback
}

func prMergedAt(pr *PRInfo, branchTip string) bool {
	return pr.State == "merged" && strings.EqualFold(pr.HeadSha, branchTip)
}
