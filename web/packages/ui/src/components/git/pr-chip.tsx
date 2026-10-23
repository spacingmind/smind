import { ForgeSettingsLink } from "@/components/git/forge-settings-link";
import { prChipModel } from "@/components/git/pr-chip-model";
import { Icons } from "@/lib/icons";
import type { TaskGitPr } from "@/lib/types";

const CHIP_CLASS =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-full border border-input-border bg-surface px-3 text-ui-sm text-foreground-muted";

/** The task's pull request as one chip: `#42 · Open · ✓ checks`. Click opens the PR on the forge (https only). */
export function PrChip({ pr, nowMs }: { pr: TaskGitPr; nowMs?: number }) {
  const { label, tone, detail } = prChipModel(pr, nowMs ?? Date.now());
  const url = typeof pr.url === "string" ? pr.url : "";
  const openable = url.startsWith("https://");
  const chip = (
    <button
      type="button"
      data-testid="pr-chip"
      disabled={!openable}
      aria-label={`${label} — Open on GitHub`}
      title={detail}
      onClick={() => {
        if (openable) window.open(url, "_blank", "noopener,noreferrer");
      }}
      className={`${CHIP_CLASS} transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:hover:bg-surface${tone === "danger" ? " text-destructive" : ""}`}
    >
      <Icons.pullRequest aria-hidden className="size-3" />
      <span>{label}</span>
    </button>
  );
  // A tooltip can't hold a link: a snapshot stale because GitHub isn't signed in gets a quiet link beside the chip.
  if (pr.stale !== "unauthenticated") return chip;
  return (
    <span className="flex shrink-0 items-center gap-2">
      {chip}
      <ForgeSettingsLink label="Sign in" testId="pr-chip-settings-link" />
    </span>
  );
}
