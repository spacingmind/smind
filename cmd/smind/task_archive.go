package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spacingmind/smind/internal/workspace"
)

// cmdTaskArchiveUsage is printed on any argument error in cmdTaskArchive.
const cmdTaskArchiveUsage = "usage: smind task archive <taskId> [--delete-branch] [--force]"

// cmdTaskArchive archives a task, mirroring the web archive dialog: it asks
// the daemon for task.archivePreview, prints the risk lines the dialog shows,
// then calls task.archive. The CLI adds no policy: the daemon archives a plain
// task whatever the risks (uncommitted work is checkpointed onto its branch),
// decides whether --delete-branch is allowed and what --force relaxes, and its
// typed refusal sentence is printed verbatim.
func cmdTaskArchive(args []string) int {
	var idArg string
	var deleteBranch, force bool
	for _, a := range args {
		switch {
		case a == "--delete-branch":
			deleteBranch = true
		case a == "--force":
			force = true
		case len(a) > 0 && a[0] == '-':
			fmt.Fprintf(os.Stderr, "task archive: unknown flag %q\n", a)
			fmt.Fprintln(os.Stderr, cmdTaskArchiveUsage)
			return 2
		case idArg == "":
			idArg = a
		default:
			fmt.Fprintln(os.Stderr, cmdTaskArchiveUsage)
			return 2
		}
	}
	if idArg == "" {
		fmt.Fprintln(os.Stderr, cmdTaskArchiveUsage)
		return 2
	}
	id, err := parseInt64(idArg)
	if err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "task archive: invalid taskId %q\n", idArg)
		fmt.Fprintln(os.Stderr, cmdTaskArchiveUsage)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var preview workspace.ArchivePreview
	if err := client.Call(context.Background(), "task.archivePreview", map[string]any{"taskId": id}, &preview); err != nil {
		fmt.Fprintf(os.Stderr, "task archive: %v\n", err)
		return 1
	}
	renderArchivePreview(os.Stdout, preview, deleteBranch)

	params := map[string]any{"taskId": id, "deleteBranch": deleteBranch, "force": force}
	if err := client.Call(context.Background(), "task.archive", params, nil); err != nil {
		fmt.Fprintf(os.Stderr, "task archive: %v\n", err)
		return 1
	}
	fmt.Printf("archived task %d\n", id)
	return 0
}

// archiveCommits is "1 commit" / "3 commits".
func archiveCommits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// renderArchivePreview prints the same risk rows, in the same order and
// wording, as the web archive dialog (archiveRows in archive-preview.tsx), then
// one line on whether the branch will be deleted.
func renderArchivePreview(w io.Writer, p workspace.ArchivePreview, deleteBranch bool) {
	r := p.Risks
	if u := r.Uncommitted; u.Files > 0 {
		fmt.Fprintf(w, "  Uncommitted changes (%d added, %d deleted lines) - saved to the branch first\n", u.Added, u.Deleted)
	}
	if r.CommitsAheadOfOrigin > 0 {
		fmt.Fprintf(w, "! %s not pushed\n", archiveCommits(r.CommitsAheadOfOrigin))
	} else if !r.Published && r.CommitsAheadOfBase > 0 {
		fmt.Fprintf(w, "! %s only on this machine (the branch was never pushed)\n", archiveCommits(r.CommitsAheadOfBase))
	}
	if name := p.Branch.Name; name != "" {
		switch p.Merged.State {
		case "merged":
			line := fmt.Sprintf("  Branch %s is merged", name)
			if p.Base.Ref != "" {
				line += " into " + p.Base.Ref
			}
			if p.Merged.Via == "pr" {
				line += " (its pull request was merged)"
			}
			fmt.Fprintln(w, line)
		case "unmerged":
			fmt.Fprintf(w, "! Branch %s is not merged anywhere\n", name)
		}
	}
	if p.IgnoredFilesLost {
		fmt.Fprintln(w, "! Files git ignores (node_modules, .env) are not kept")
	}
	if p.HasTeardown {
		fmt.Fprintln(w, "  The workspace's teardown script runs first")
	}
	switch {
	case !deleteBranch:
		fmt.Fprintln(w, "  The branch is kept")
	case p.CanDeleteBranch:
		fmt.Fprintf(w, "  Branch %s will be deleted\n", p.Branch.Name)
	case p.DeleteBlockedReason != "":
		fmt.Fprintf(w, "! Branch deletion is blocked without --force: %s\n", p.DeleteBlockedReason)
	default:
		fmt.Fprintln(w, "! Branch deletion is blocked without --force")
	}
}
