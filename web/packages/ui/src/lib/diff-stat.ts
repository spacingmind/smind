/** The headline numbers for a task's whole diff: how many files it touches and how many lines it adds/removes. */
export interface DiffStat {
  files: number;
  additions: number;
  deletions: number;
}

export const EMPTY_DIFF_STAT: DiffStat = { files: 0, additions: 0, deletions: 0 };

/**
 * Counts files/+lines/-lines out of a unified diff (ui-redesign-parity
 * plan, Item 19's "a diff stat surfaced outside this pane").
 *
 * Derived client-side from the `task.diff` text rather than added as a
 * daemon numstat RPC: the diff pane already has to fetch that text for
 * its whole-diff view, so this costs nothing extra on the surface that
 * needs it most, and it stays additive (no wire change, no rule-(d)
 * gate). A per-row stat for every task in the sidebar (Item 12) would be
 * a different tradeoff -- see the plan's Item 19 decisions.
 *
 * `+++ `/`--- ` file headers are excluded from the line counts, and a
 * `diff --git` header is what counts as a file; a diff produced without
 * those headers falls back to counting `+++ ` lines, so a stat is never
 * silently zero for a diff that plainly has content.
 */
export function parseDiffStat(diff: string): DiffStat {
  if (!diff) return EMPTY_DIFF_STAT;

  let files = 0;
  let headerFiles = 0;
  let additions = 0;
  let deletions = 0;

  for (const line of diff.split("\n")) {
    if (line.startsWith("diff --git ")) {
      files++;
    } else if (line.startsWith("+++ ")) {
      headerFiles++;
    } else if (line.startsWith("--- ")) {
      // A file header, not a deletion.
    } else if (line.startsWith("+")) {
      additions++;
    } else if (line.startsWith("-")) {
      deletions++;
    }
  }

  return { files: files || headerFiles, additions, deletions };
}

/** "3 files +42 −7" -- the one place this is spelled, so the pane header and any later consumer (sidebar, composer) read identically. */
export function formatDiffStat(stat: DiffStat): string {
  return `${stat.files} file${stat.files === 1 ? "" : "s"} +${stat.additions} −${stat.deletions}`;
}
