import { SettingsEmptyCard, SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
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

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * Settings -> General -> "Scratch space": how much disk the per-task scratch folders use and how much of
 * it a clean-up would free. The folders of closed or unknown tasks are reclaimable; a running task's folder
 * never is, and the daemon refuses to touch it, so a refusal is shown exactly as the daemon worded it.
 * The numbers are read when the card opens and again after every clean.
 */
export function ScratchSpaceGroup() {
  const { usage, loading, error, refresh } = useScratchUsage();
  const { clean, pending, result, error: cleanError } = useScratchClean();

  return (
    <SettingsGroup
      title="Scratch space"
      description="Temporary files agents write for a task, kept per task outside the worktree."
      testId="scratch-space"
    >
      {loading && !usage ? (
        <div className="px-4 py-3" data-testid="scratch-space-loading">
          <LoadingRows rows={2} label="Loading scratch usage…" />
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
        <div className="p-3" data-testid="scratch-space-empty">
          <SettingsEmptyCard
            title="No scratch files"
            description="Nothing is stored for any task. Files agents write here will be listed when they do."
          />
        </div>
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
                  data-testid="scratch-space-clean"
                >
                  {pending ? "Cleaning…" : "Clean up"}
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
                    : `Freed ${formatScratchBytes(result.bytes)} from ${plural(result.dirs, "task folder", "task folders")}.`}
                </p>
              ) : null}
            </div>
          )}
        </>
      ) : null}
    </SettingsGroup>
  );
}
