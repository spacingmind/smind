import { Icons } from "@/lib/icons";

import type { GitRule, PermissionDetail } from "@/lib/types";
import { cn } from "@/lib/utils";

function kb(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function lines(n: number): string {
  return `${n} ${n === 1 ? "line" : "lines"}`;
}

/** One-line size summary: `240 → 7 lines · 3.1 KB → 0.2 KB`, or `new file · 12 lines · 0.4 KB`. */
function sizeSummary(d: PermissionDetail): string | null {
  const hasExisting = d.existingLines !== undefined || d.existingBytes !== undefined;
  const parts: string[] = [];
  if (!hasExisting && !d.replacesExisting && d.kind === "write") parts.push("new file");
  if (d.newLines !== undefined) {
    parts.push(d.existingLines !== undefined ? `${d.existingLines} → ${lines(d.newLines)}` : lines(d.newLines));
  }
  if (d.newBytes !== undefined) {
    parts.push(d.existingBytes !== undefined ? `${kb(d.existingBytes)} → ${kb(d.newBytes)}` : kb(d.newBytes));
  }
  return parts.length > 0 ? parts.join(" · ") : null;
}

function Badge({ children, testId, danger }: { children: string; testId: string; danger?: boolean }) {
  return (
    <span
      data-testid={testId}
      className={cn(
        "rounded border px-1.5 py-0.5 text-ui-xs font-medium",
        danger ? "border-destructive/40 text-destructive" : "border-warning/40 text-warning",
      )}
    >
      {children}
    </span>
  );
}

function lineClass(line: string): string {
  if (line.startsWith("+")) return "text-diff-added";
  if (line.startsWith("-")) return "text-diff-removed";
  if (line.startsWith("@@")) return "text-foreground-muted";
  return "text-foreground";
}

/** Plain-text, line-by-line; never HTML. Long lines scroll rather than widen the card. */
function Pre({ text, colored, testId }: { text: string; colored?: boolean; testId: string }) {
  return (
    <pre
      data-testid={testId}
      className="mt-1 max-h-64 overflow-auto rounded border border-border bg-surface px-2 py-1 font-mono text-ui-xs"
    >
      {colored
        ? text.split("\n").map((line, i) => (
            <span key={i} className={cn("block", lineClass(line))}>
              {line || " "}
            </span>
          ))
        : text}
    </pre>
  );
}

const MAX_STAGED_SHOWN = 8;

/** Quiet "why am I being asked" block for a git approval rule (G13). */
function GitRuleView({ rule }: { rule: GitRule }) {
  const files = rule.stagedFiles ?? [];
  const shown = files.slice(0, MAX_STAGED_SHOWN);
  const more = files.length - shown.length + (rule.stagedTruncated ? 1 : 0);
  const label = rule.class === "commit" ? `${rule.label} · asks before committing` : rule.label;
  return (
    <div data-testid="permission-git-rule" className="flex flex-col gap-1">
      <div className="flex items-center gap-1.5 text-ui-xs text-foreground-muted" data-testid="permission-git-rule-label">
        <Icons.permission className="size-3.5 shrink-0" />
        <span>{label}</span>
      </div>
      <pre className="max-h-[5.5rem] overflow-auto rounded border border-border bg-surface px-2 py-1 font-mono text-ui-xs" data-testid="permission-git-rule-command">
        {rule.command}
      </pre>
      {(rule.remote || rule.branch) && (
        <div className="text-ui-xs text-foreground-muted" data-testid="permission-git-rule-target">
          {[rule.remote && `Remote ${rule.remote}`, rule.branch && `Branch ${rule.branch}`].filter(Boolean).join(" · ")}
        </div>
      )}
      {shown.length > 0 && (
        <ul className="m-0 list-none p-0 font-mono text-ui-xs" data-testid="permission-git-rule-staged">
          {shown.map((f) => (
            <li key={f} className="truncate" title={f}>
              {f}
            </li>
          ))}
          {more > 0 && <li className="text-foreground-muted">{`+${more} more`}</li>}
        </ul>
      )}
      {rule.note && (
        <div className="text-ui-xs text-foreground-muted" data-testid="permission-git-rule-note">
          {rule.note}
        </div>
      )}
    </div>
  );
}

/**
 * The structured "what is this write/edit/command about to do" block shown
 * inside a permission card (dogfood-gap-fixes AC3). Rendered only when the
 * daemon sent a `detail`.
 */
export function PermissionDetailView({ detail }: { detail: PermissionDetail }) {
  const size = sizeSummary(detail);
  const body = detail.diff || detail.preview;
  const bodyTitle = detail.diff ? "Diff" : "Preview";

  return (
    <div data-testid="permission-detail" className="mt-1 flex flex-col gap-1.5 text-ui-sm text-foreground">
      {detail.path && (
        <div title={detail.path} className="truncate font-mono text-ui-xs" data-testid="permission-detail-path">
          {detail.path}
        </div>
      )}
      {size && (
        <div className="text-foreground-muted" data-testid="permission-detail-size">
          {size}
        </div>
      )}
      {(detail.replacesExisting || detail.outsideWorktree) && (
        <div className="flex flex-wrap gap-1.5">
          {detail.replacesExisting && <Badge testId="permission-detail-replaces">Replaces existing file</Badge>}
          {detail.outsideWorktree && <Badge testId="permission-detail-outside">Outside the task folder</Badge>}
        </div>
      )}
      {detail.largeShrink && detail.existingLines !== undefined && detail.newLines !== undefined && (
        <div
          role="alert"
          data-testid="permission-detail-shrink"
          className="flex items-start gap-1.5 rounded border border-destructive/40 px-2 py-1 font-medium text-destructive"
        >
          <Icons.warning className="mt-0.5 size-3.5 shrink-0" />
          <span>{`Overwrites ${detail.existingLines} lines with ${detail.newLines} — most of the file will be lost`}</span>
        </div>
      )}
      {detail.redacted && (
        <div className="text-foreground-muted" data-testid="permission-detail-redacted">
          Existing contents hidden (restricted path)
        </div>
      )}
      {body && (
        <details open={detail.largeShrink && !!detail.diff} data-testid="permission-detail-body">
          <summary className="cursor-pointer text-foreground-muted">{bodyTitle}</summary>
          <Pre text={body} colored={!!detail.diff} testId="permission-detail-pre" />
          {detail.truncated && (
            <div className="mt-1 text-foreground-muted" data-testid="permission-detail-truncated">
              Truncated
            </div>
          )}
        </details>
      )}
      {detail.gitRule && <GitRuleView rule={detail.gitRule} />}
      {detail.command && !detail.gitRule && (
        <div data-testid="permission-detail-command">
          <pre className="max-h-[5.5rem] overflow-auto rounded border border-border bg-surface px-2 py-1 font-mono text-ui-xs">
            {detail.command}
          </pre>
          {detail.truncated && <div className="mt-1 text-foreground-muted" data-testid="permission-detail-truncated">Truncated</div>}
        </div>
      )}
    </div>
  );
}
