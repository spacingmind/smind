import type { ReactNode } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { Icons } from "@/lib/icons";
import type { ArchivePreview } from "@/lib/types";
import { cn } from "@/lib/utils";

/** `1 commit` / `3 commits`. */
function commits(n: number): string {
  return `${n} commit${n === 1 ? "" : "s"}`;
}

/** The names the dialog quotes (branch, base) are always text, in a quiet mono span. */
function Name({ children }: { children: string }) {
  return <code className="rounded bg-tag/50 px-1 font-mono text-ui-sm text-foreground">{children}</code>;
}

interface Row {
  key: string;
  warn: boolean;
  text: ReactNode;
}

/** The risk rows the archive dialog lists, in the order a user weighs them: lost work first, then where the branch stands. */
export function archiveRows(preview: ArchivePreview): Row[] {
  const { risks, branch, base, merged } = preview;
  const rows: Row[] = [];
  const { files, added, deleted } = risks.uncommitted;
  if (files > 0) {
    rows.push({
      key: "uncommitted",
      warn: false,
      text: `Uncommitted changes (${added} added, ${deleted} deleted lines) — saved to the branch first`,
    });
  }
  if (risks.commitsAheadOfOrigin > 0) {
    rows.push({ key: "unpushed", warn: true, text: `${commits(risks.commitsAheadOfOrigin)} not pushed` });
  } else if (!risks.published && risks.commitsAheadOfBase > 0) {
    rows.push({ key: "unpublished", warn: true, text: `${commits(risks.commitsAheadOfBase)} only on this machine (the branch was never pushed)` });
  }
  if (branch.name !== "") {
    if (merged.state === "merged") {
      rows.push({
        key: "merged",
        warn: false,
        text: (
          <>
            Branch <Name>{branch.name}</Name> is merged{base.ref !== "" && <> into <Name>{base.ref}</Name></>}
            {merged.via === "pr" && " (its pull request was merged)"}
          </>
        ),
      });
    } else if (merged.state === "unmerged") {
      rows.push({
        key: "unmerged",
        warn: true,
        text: (
          <>
            Branch <Name>{branch.name}</Name> is not merged anywhere
          </>
        ),
      });
    }
  }
  if (preview.ignoredFilesLost) {
    rows.push({ key: "ignored", warn: true, text: "Files git ignores (node_modules, .env) are not kept" });
  }
  if (preview.hasTeardown) rows.push({ key: "teardown", warn: false, text: "The workspace's teardown script runs first" });
  return rows;
}

export function ArchiveRisks({ preview }: { preview: ArchivePreview }) {
  return (
    <ul className="flex flex-col gap-1.5 text-ui-base text-foreground-muted" data-testid="archive-risks">
      {archiveRows(preview).map((row) => (
        <li key={row.key} className="flex items-start gap-2" data-testid={`archive-risk-${row.key}`}>
          {row.warn ? (
            <Icons.warning aria-hidden className="mt-0.5 size-3.5 shrink-0 text-warning" />
          ) : (
            <Icons.info aria-hidden className="mt-0.5 size-3.5 shrink-0 text-foreground-subtle" />
          )}
          <span className="min-w-0 break-words">{row.text}</span>
        </li>
      ))}
    </ul>
  );
}

/** What the delete-branch control sends: nothing, a plain delete, or a forced one. */
export interface DeleteChoice {
  deleteBranch: boolean;
  force: boolean;
}

/** Why an owned branch would be lost by "anyway": the unmerged commits, else the changes the checkpoint saves on it. */
function forceCost(preview: ArchivePreview): string {
  const ahead = preview.risks.commitsAheadOfBase;
  if (preview.merged.state !== "merged" && ahead > 0) return `${commits(ahead)} ${ahead === 1 ? "is" : "are"} not merged anywhere`;
  if (preview.risks.uncommitted.files > 0) return "the saved uncommitted changes would be lost";
  return "it is not merged anywhere";
}

/**
 * The Delete branch control (ADR-0039 §9). The daemon decides: `canDeleteBranch` gives a checkbox (on only when
 * `deleteBranchDefault`); otherwise a branch smind didn't create gets a disabled one with the daemon's reason, and an
 * owned one an unchecked destructive "anyway" that sends `force`. Nothing is deleted without the user seeing it.
 */
export function DeleteBranchOption({
  preview,
  checked,
  disabled,
  onChange,
}: {
  preview: ArchivePreview;
  checked: boolean;
  disabled: boolean;
  onChange: (next: boolean) => void;
}) {
  const name = preview.branch.name;
  if (name === "") return null;
  const reason = preview.deleteBlockedReason;
  if (preview.canDeleteBranch) {
    return (
      <label className="flex items-center gap-2 text-ui-base" data-testid="archive-delete-branch-label">
        <Checkbox checked={checked} disabled={disabled} onCheckedChange={(on) => onChange(on === true)} data-testid="archive-delete-branch" />
        <span>
          Delete branch <Name>{name}</Name>
        </span>
      </label>
    );
  }
  if (!preview.branch.owned) {
    return (
      <div className="flex flex-col gap-1" data-testid="archive-delete-branch-blocked">
        <label className="flex items-center gap-2 text-ui-base text-foreground-subtle">
          <Checkbox checked={false} disabled data-testid="archive-delete-branch" />
          <span>
            Delete branch <Name>{name}</Name>
          </span>
        </label>
        {reason && (
          <p className="pl-6 text-ui-sm text-foreground-muted" data-testid="archive-delete-branch-reason">
            {reason}
          </p>
        )}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-1" data-testid="archive-delete-branch-blocked">
      <label className={cn("flex items-center gap-2 text-ui-base text-destructive")}>
        <Checkbox checked={checked} disabled={disabled} onCheckedChange={(on) => onChange(on === true)} data-testid="archive-delete-branch-force" />
        <span>Delete the branch anyway — {forceCost(preview)}</span>
      </label>
      {reason && preview.merged.state === "merged" && (
        <p className="pl-6 text-ui-sm text-foreground-muted" data-testid="archive-delete-branch-reason">
          {reason}
        </p>
      )}
    </div>
  );
}
