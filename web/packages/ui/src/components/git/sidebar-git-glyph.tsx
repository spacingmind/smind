import { Icons } from "@/lib/icons";
import type { TaskGit } from "@/lib/types";

function prGlyph(pr: NonNullable<TaskGit["pr"]>): { label: string; className: string } {
  const failing = (pr.checks?.failed ?? 0) > 0 || pr.reviewDecision === "changes_requested";
  const state = pr.state === "merged" ? "merged" : pr.state === "closed" ? "closed" : pr.draft ? "draft" : "open";
  const tone =
    state === "open" && failing
      ? "text-destructive"
      : state === "open"
        ? "text-foreground-muted"
        : state === "merged"
          ? "text-foreground"
          : "text-foreground-subtlest";
  return { label: `Pull request #${pr.number} ${state}`, className: tone };
}

/** The sidebar row's git hint: at most one PR glyph and one status dot. Colour never carries state alone — each has a text label. */
export function SidebarGitGlyph({ git }: { git: TaskGit }) {
  const pr = git.pr ? prGlyph(git.pr) : null;
  const base = git.base?.ref || "base";
  const dot =
    git.conflict?.state === "conflicts"
      ? { label: `Conflicts with ${base}`, className: "bg-destructive" }
      : git.origin?.published === false && (git.commits?.sinceFork ?? 0) > 0
        ? { label: "Not published", className: "bg-foreground-subtlest" }
        : null;
  if (!pr && !dot) return null;
  return (
    <span data-testid="sidebar-git-glyph" className="inline-flex shrink-0 items-center gap-1">
      {pr && (
        <span role="img" aria-label={pr.label} title={pr.label} className={`inline-flex ${pr.className}`}>
          <Icons.pullRequest aria-hidden className="size-3" />
        </span>
      )}
      {dot && (
        <span
          data-testid="sidebar-git-dot"
          role="img"
          aria-label={dot.label}
          title={dot.label}
          className={`size-1.5 rounded-full ${dot.className}`}
        />
      )}
    </span>
  );
}
