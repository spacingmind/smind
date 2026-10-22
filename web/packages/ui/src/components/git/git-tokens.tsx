import type { ReactNode } from "react";

import type { TaskGit } from "@/lib/types";

/** One mono line of git facts for a task: diff, commits, publish state, base drift, conflicts. Unknown = absent, never zero. */
export function GitTokens({ git }: { git: TaskGit }) {
  const tokens: ReactNode[] = [];
  const base = git.base?.ref || "base";

  const ins = git.diff?.insertions ?? 0;
  const del = git.diff?.deletions ?? 0;
  if (ins > 0 || del > 0) {
    tokens.push(
      <span key="diff">
        <span className="text-success">+{ins}</span> <span className="text-destructive">−{del}</span>
      </span>,
    );
  }

  const commits = git.commits?.sinceFork ?? 0;
  if (commits > 0) tokens.push(<span key="commits">{commits === 1 ? "1 commit" : `${commits} commits`}</span>);

  if (git.origin?.published === false) {
    tokens.push(<span key="publish">not published</span>);
  } else if (git.origin) {
    const ahead = git.origin.ahead ?? 0;
    const behind = git.origin.behind ?? 0;
    if (ahead > 0 || behind > 0) {
      tokens.push(
        <span key="publish">
          {ahead > 0 ? `↑${ahead}` : ""}
          {ahead > 0 && behind > 0 ? " " : ""}
          {behind > 0 ? `↓${behind}` : ""}
        </span>,
      );
    }
  }

  const behindBase = git.commits?.behindBase ?? 0;
  if (behindBase > 0) tokens.push(<span key="behind">{`↓${behindBase} behind ${base}`}</span>);

  if (git.conflict?.state === "conflicts") {
    tokens.push(
      <span key="conflict" className="text-destructive">
        {`⚠ conflicts with ${base}`}
      </span>,
    );
  }

  if (tokens.length === 0) return null;
  return (
    <p
      data-testid="git-tokens"
      className="flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-ui-xs tabular-nums text-foreground-muted"
    >
      {tokens.flatMap((token, i) => (i === 0 ? [token] : [<span key={`sep-${i}`} aria-hidden>·</span>, token]))}
    </p>
  );
}
