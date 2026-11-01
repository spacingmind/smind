import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Skeleton } from "@/components/ui/skeleton";
import { Icons } from "@/lib/icons";
import { useScratchClean, useScratchUsage } from "@/hooks/use-scratch";

const UNITS = ["KiB", "MiB", "GiB", "TiB"] as const;

/** "0 B" / "512 B" / "2.0 KiB" / "10 MiB" / "1.5 GiB": one decimal below 10, whole numbers from there. */
export function formatScratchBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${UNITS[unit]}`;
}

/** One skeleton bar centred in a block as tall as the text line it stands in for, so the loading card is as tall as the loaded one. */
function SkeletonLine({ className, bar }: { className: string; bar: string }) {
  return (
    <span className={`flex items-center ${className}`}>
      <Skeleton aria-hidden className={`${bar} bg-foreground/15`} />
    </span>
  );
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * Settings -> General -> "Scratch space": how much disk the per-task scratch folders use and how much of
 * it a clean-up would free. The folders of closed or unknown tasks are reclaimable; a running task's folder
 * never is. The daemon's sweep skips a busy task's folder without an error (only removing one task's folder
 * answers "scratch: task has a running run"), so a skip shows as "kept" after the re-read, and any error the
 * daemon does answer is shown exactly as worded.
 * The numbers are read when the card opens and again after every clean.
 */
export function ScratchSpaceGroup() {
  const { usage, loading, refreshing, error, refresh } = useScratchUsage();
  const { clean, pending, result, error: cleanError } = useScratchClean();

  return (
    <SettingsGroup
      title="Scratch space"
      description="Temporary files agents write for a task, kept per task outside the worktree."
      testId="scratch-space"
    >
      {loading && !usage ? (
        // Two skeleton rows with the loaded rows' own layout (label, one or two description lines, a control), so the
        // card keeps its height when the numbers land. Visible against the card in both themes (foreground tint).
        <div role="status" data-testid="scratch-space-loading">
          <span className="sr-only">Loading scratch usage…</span>
          <SettingsRow
            label={<SkeletonLine className="h-6" bar="h-4 w-24" />}
            description={<SkeletonLine className="h-5" bar="h-3 w-44" />}
            control={<SkeletonLine className="h-6" bar="h-4 w-14" />}
          />
          <SettingsRow
            label={<SkeletonLine className="h-6" bar="h-4 w-24" />}
            description={
              <>
                <SkeletonLine className="h-5" bar="h-3 w-60" />
                <SkeletonLine className="h-5" bar="h-3 w-36" />
              </>
            }
            control={<SkeletonLine className="h-8" bar="h-7 w-24" />}
          />
        </div>
      ) : error && !usage ? (
        <SettingsRow
          label="Couldn't read scratch usage"
          description={<FormError message={error} testId="scratch-space-error" size="sm" />}
          control={
            <Button variant="outline" size="sm" onClick={refresh} data-testid="scratch-space-retry">
              Retry
            </Button>
          }
        />
      ) : usage && usage.dirs === 0 ? (
        <SettingsRow
          testId="scratch-space-empty"
          label="No scratch files"
          description="Nothing is stored for any task. Files agents write here will be listed when they do."
          control={<span className="text-ui-base font-medium tabular-nums text-foreground-muted">0 B</span>}
        />
      ) : usage ? (
        <>
          <SettingsRow
            label="Disk usage"
            description={`${plural(usage.dirs, "task folder", "task folders")} · ${plural(usage.totalFiles, "file", "files")}`}
            control={
              <span className="text-ui-base font-medium tabular-nums text-foreground" data-testid="scratch-space-total">
                {formatScratchBytes(usage.totalBytes)}
              </span>
            }
          />
          <SettingsRow
            label="Reclaimable"
            description={`From ${plural(usage.reclaimableDirs, "closed or unknown task", "closed or unknown tasks")}. A running task's folder is never touched.`}
            control={
              <div className="flex items-center gap-3">
                <span className="text-ui-base font-medium tabular-nums text-foreground" data-testid="scratch-space-reclaimable">
                  {formatScratchBytes(usage.reclaimableBytes)}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={clean}
                  disabled={pending || usage.reclaimableDirs === 0}
                  className={pending ? "disabled:opacity-80" : undefined}
                  data-testid="scratch-space-clean"
                >
                  {pending ? (
                    <>
                      <Icons.spinner aria-hidden data-testid="scratch-space-spinner" className="size-3.5 animate-spin" />
                      Cleaning…
                    </>
                  ) : (
                    "Clean up"
                  )}
                </Button>
              </div>
            }
          />
          {(result || cleanError) && (
            <div className="px-4 py-3">
              {cleanError ? (
                <FormError message={cleanError} testId="scratch-space-clean-error" size="sm" />
              ) : result ? (
                <p role="status" className="text-ui-sm text-foreground-muted" data-testid="scratch-space-result">
                  {result.bytes === 0 && result.files === 0
                    ? "Nothing to clean up."
                    : `Freed ${formatScratchBytes(result.bytes)} (${plural(result.files, "file", "files")}).`}
                  {!refreshing && usage.reclaimableDirs > 0 &&
                    ` ${plural(usage.reclaimableDirs, "task folder was", "task folders were")} kept because a run is still using ${usage.reclaimableDirs === 1 ? "it" : "them"}; clean up again when it ends.`}
                </p>
              ) : null}
            </div>
          )}
        </>
      ) : null}
    </SettingsGroup>
  );
}
