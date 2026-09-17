import type { TaskFile } from "@/lib/types";

/**
 * A non-LLM suggested commit message for the commit bar (visual-identity
 * plan Item 8, per uiux-audit.md P2.12 / Conductor's "Suggested Git
 * Actions" precedent): staged file count + dominant change kind, no model
 * call.
 *
 * Considers only *staged* files -- those are what task.commit records --
 * so the pane passes its whole task.files list and the staged filter
 * happens here rather than at the call site; that keeps the heuristic
 * testable with plain TaskFile fixtures and makes the suggestion agree
 * with the "Commit (N staged)" count by construction.
 */
export function suggestCommitMessage(files: readonly TaskFile[]): string | null {
  const staged = files.filter((f) => f.staged);
  if (staged.length === 0) return null;

  if (staged.length === 1) {
    const [file] = staged;
    return `${verbFor(file.status) ?? "Update"} ${file.path}`;
  }

  // A uniform list names its kind ("Add 3 files"); a mixed one claims no
  // kind -- "Update 3 files" -- since naming even the dominant kind would
  // misdescribe the rest.
  const kinds = new Set(staged.map((f) => f.status));
  const verb = kinds.size === 1 ? verbFor(staged[0].status) : "Update";
  return `${verb ?? "Update"} ${staged.length} files`;
}

/**
 * One change kind -> imperative verb, or null for anything else. TaskFile's
 * status is widened past the three kinds the daemon emits today
 * (git name-status codes are lowercased verbatim), so an unknown kind
 * falls back to "Update" at the call site rather than guessing.
 */
function verbFor(status: string): string | null {
  switch (status) {
    case "added":
      return "Add";
    case "deleted":
      return "Remove";
    case "modified":
      return "Update";
    default:
      return null;
  }
}
