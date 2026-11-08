import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/ui/status-badge";
import { useIsMobile } from "@/hooks/use-mobile";
import { useStewardFrame, useStewardReview } from "@/hooks/use-steward";
import { openExternalUrl } from "@/lib/external-link";
import { Icons } from "@/lib/icons";
import type { InboxItem } from "@/lib/inbox-types";
import { queryKeys } from "@/lib/query-keys";
import {
  axisLabel,
  axisStatusLabel,
  blockedReasonLabel,
  criteriaCounts,
  findingsAsPrompt,
  groupFindingsByAxisThenFile,
  groupFindingsByFile,
  sortGatesFailingFirst,
  verdictLabel,
  type StewardAxis,
  type StewardCriterion,
  type StewardFinding,
  type StewardFrame,
  type StewardGate,
} from "@/lib/steward-types";
import type { TaskGit } from "@/lib/types";

/** The verdict as the row and card chip: an icon plus the always-visible label; an unknown verdict renders as its raw text. */
export function VerdictChip({ verdict }: { verdict: string | undefined }) {
  const { text, tone } = verdictLabel(verdict);
  const Icon =
    verdict === "ACCEPTED" ? Icons.check : verdict === "CHANGES" ? Icons.warning : verdict === "BLOCKED" ? Icons.permissionDanger : Icons.verdict;
  return (
    <StatusBadge status={tone} data-testid="steward-verdict-chip">
      <Icon aria-hidden className="size-3" />
      {text}
    </StatusBadge>
  );
}

/** A pass's chip: icon plus the always-visible status text, in the tone of its seat verdict. */
export function AxisChip({ axis, testId }: { axis: Pick<StewardAxis, "status" | "seatVerdict" | "blockedReason">; testId?: string }) {
  const { text, tone } = axisStatusLabel(axis);
  const Icon =
    text === "Accepted" ? Icons.check : text === "Changes requested" ? Icons.warning : tone === "danger" ? Icons.permissionDanger : Icons.verdict;
  return (
    <StatusBadge status={tone} data-testid={testId}>
      <Icon aria-hidden className="size-3" />
      {text}
    </StatusBadge>
  );
}

/** The inbox row's per-pass chips: "Standards: Accepted" style, icon plus text, wrapping. */
export function ReviewRowAxes({ axes }: { axes: { axis: string; verdict?: string; status?: string }[] }) {
  return (
    <span className="flex flex-wrap items-center gap-1" data-testid="inbox-review-axes">
      {axes.map((a, i) => {
        const { text, tone } = axisStatusLabel({ status: a.status ?? (a.verdict ? "done" : "pending"), seatVerdict: a.verdict });
        const Icon = text === "Accepted" ? Icons.check : text === "Changes requested" ? Icons.warning : tone === "danger" ? Icons.permissionDanger : Icons.verdict;
        return (
          <StatusBadge key={`${a.axis}-${i}`} status={tone} className="px-1.5 py-0" data-testid={`inbox-review-axis-${a.axis}`}>
            <Icon aria-hidden className="size-3" />
            {axisLabel(a.axis)}: {text}
          </StatusBadge>
        );
      })}
    </span>
  );
}

/** One spec criterion: an icon, the status word, the criterion id and its evidence. */
function CriterionLine({ criterion }: { criterion: StewardCriterion }) {
  const met = criterion.status === "met";
  const unmet = criterion.status === "unmet";
  const Icon = met ? Icons.check : unmet ? Icons.permissionDanger : Icons.verdict;
  return (
    <li className="flex items-start gap-1.5" data-testid={`steward-criterion-${criterion.id}`}>
      <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0 break-words">
        {met ? "Met" : unmet ? "Unmet" : "Unverifiable"} <span className="font-mono">{criterion.id}</span>
        {criterion.evidence && <span className="text-foreground-muted"> — {criterion.evidence}</span>}
      </span>
    </li>
  );
}

/** One review pass: name, chip, summary, model, the reason it did not complete, and the spec checklist. */
function AxisRow({ axis }: { axis: StewardAxis }) {
  const criteria = axis.criteria ?? [];
  const counts = criteriaCounts(criteria);
  const showReason = axis.status === "blocked" && axis.blockedReason;
  return (
    <li className="flex flex-col gap-1" data-testid={`steward-axis-${axis.axis}`}>
      <span className="flex flex-wrap items-center gap-1.5">
        <span className="font-medium">{axisLabel(axis.axis)}</span>
        <AxisChip axis={axis} />
        {axis.model && <span className="font-mono text-ui-xs text-foreground-muted">{axis.model}</span>}
      </span>
      {axis.summary && <p className="m-0">{axis.summary}</p>}
      {showReason && <p className="m-0 text-destructive">{blockedReasonLabel(axis.blockedReason)}</p>}
      {criteria.length > 0 && (
        <>
          <span className="text-ui-xs text-foreground-muted" data-testid="steward-criteria-count">
            {counts.met} met, {counts.unmet} unmet
            {counts.unverifiable > 0 ? `, ${counts.unverifiable} unverifiable` : ""}
          </span>
          <ul className="m-0 flex flex-col gap-1 p-0" data-testid="steward-criteria">
            {criteria.map((c) => (
              <CriterionLine key={c.id} criterion={c} />
            ))}
          </ul>
        </>
      )}
    </li>
  );
}

/** The per-file finding blocks, as the card has always drawn them (a `rule` adds one muted line). */
function FileBlocks({ files }: { files: { file: string; findings: StewardFinding[] }[] }) {
  return (
    <>
      {files.map((g) => (
        <div key={g.file} className="flex flex-col gap-1">
          <h4 className="m-0 font-mono text-ui-xs font-medium">{g.file}</h4>
          <ul className="m-0 flex flex-col gap-1.5 p-0">
            {g.findings.map((f, i) => (
              <li key={`${f.line}-${i}`} className="flex flex-col gap-0.5">
                <span className="flex flex-wrap items-center gap-1.5">
                  <StatusBadge status="neutral" className="px-1.5 py-0" data-testid="steward-finding-severity">
                    {f.severity || "unrated"}
                  </StatusBadge>
                  <span>
                    line {f.line}: {f.why}
                  </span>
                </span>
                <span className="text-ui-xs text-foreground-muted">check: {f.check}</span>
                {f.rule && <span className="text-ui-xs text-foreground-muted">rule: {f.rule}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </>
  );
}

/** The review row's kind pill: the verdict itself once the review is loaded, a quiet "Review" until then. */
export function ReviewRowChip({ item }: { item: InboxItem }) {
  const review = useStewardReview(item.links.reviewId);
  if (review.isPending || review.isError || !review.data) return <StatusBadge status="neutral">Review</StatusBadge>;
  return <VerdictChip verdict={review.data.verdict} />;
}

/** One gate of the checklist: an icon for the state, the gate's id, the state text, and the daemon's detail when it has one. */
function GateLine({ gate }: { gate: StewardGate }) {
  const Icon = gate.state === "fail" ? Icons.permissionDanger : gate.state === "pass" ? Icons.check : Icons.verdict;
  return (
    <li className="flex items-start gap-1.5" data-testid={`steward-gate-${gate.id}`}>
      <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>
        <span className="font-mono">{gate.id}</span> <span className="text-foreground-muted">{gate.state}</span>
        {gate.detail && <span className="text-foreground-muted"> — {gate.detail}</span>}
      </span>
    </li>
  );
}

/** One evidence frame, lazily fetched into a fixed-ratio slot so nothing jumps; anything but a PNG is refused. */
function FrameThumb({ reviewId, frame }: { reviewId: number; frame: StewardFrame }) {
  const img = useStewardFrame(reviewId, frame.name);
  return (
    <figure className="m-0 flex flex-col gap-1">
      {img.isPending ? (
        <Skeleton aria-hidden className="aspect-video w-full" />
      ) : img.isError || !img.data || img.data.contentType !== "image/png" ? (
        <div className="flex aspect-video w-full items-center justify-center rounded border border-border text-ui-xs text-foreground-muted">
          Frame unavailable
        </div>
      ) : (
        <img
          src={`data:${img.data.contentType};base64,${img.data.data}`}
          alt={`${frame.name} (${frame.viewport}px ${frame.theme})`}
          loading="lazy"
          data-testid={`steward-frame-${frame.name}`}
          className="w-full rounded border"
        />
      )}
      <figcaption className="text-ui-xs text-foreground-muted">
        {frame.name} · {frame.viewport}px, {frame.theme}
      </figcaption>
    </figure>
  );
}

/** The task's PR url when a cached `task.git` read already holds it (the task page's own entry); never fetches. */
function useCachedPrUrl(taskId: number | undefined): string {
  const { data } = useQuery<TaskGit>({ queryKey: queryKeys.taskGit(taskId ?? -1), enabled: false });
  const url = data?.pr?.url;
  return typeof url === "string" && url.startsWith("https://") ? url : "";
}

/**
 * The steward review behind a `review:` row (ADR-0047 §5/§9): verdict, gates, findings and evidence
 * frames, read-only. There is no approve, accept or merge control anywhere — the daemon has no such
 * method, and the Human merges outside smind.
 */
export function StewardReviewCard({ item, onOpenTask }: { item: InboxItem; onOpenTask: () => void }) {
  const review = useStewardReview(item.links.reviewId);
  const prUrl = useCachedPrUrl(item.links.taskId ?? item.taskId);
  const narrow = useIsMobile();
  const btn = narrow ? "h-11" : undefined;
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>(0);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  if (review.isPending) return <LoadingRows />;
  if (review.isError || !review.data) return <FormError message="Could not load the review." testId="steward-review-error" />;
  const data = review.data;

  // The Human copies the findings to the Lead by hand (ADR-0047 §5: nothing is auto-sent).
  const copyFindings = () => {
    void navigator.clipboard?.writeText(findingsAsPrompt(data)).then(
      () => {
        setCopied(true);
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(() => setCopied(false), 2000);
      },
      () => {},
    );
  };

  return (
    <div data-testid={`steward-review-${item.key}`} className="flex flex-col gap-2 px-1 text-ui-sm text-foreground">
      <div className="flex flex-wrap items-center gap-2">
        <VerdictChip verdict={data.verdict} />
        {data.prNumber !== undefined && <span className="text-foreground-muted">PR #{data.prNumber}</span>}
        <span className="font-mono text-ui-xs text-foreground-muted">{data.headSha.slice(0, 7)}</span>
        {data.stale && (
          <span className="text-warning-text" data-testid="steward-stale">
            Stale: {data.stale}
          </span>
        )}
      </div>
      {data.summary && <p className="m-0">{data.summary}</p>}
      {data.verdict === "BLOCKED" && data.blockedReason && (
        <p className="m-0 text-destructive" data-testid="steward-blocked-reason">
          {data.blockedReason}
        </p>
      )}
      {data.axes && data.axes.length > 0 && (
        <div className="flex flex-col gap-1" data-testid="steward-axes">
          <h3 className="m-0 text-ui-sm font-medium">Review axes</h3>
          <ul className="m-0 flex flex-col gap-2 p-0">
            {data.axes.map((a, i) => (
              <AxisRow key={`${a.axis}-${i}`} axis={a} />
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1">
        <h3 className="m-0 text-ui-sm font-medium">Gates</h3>
        <ul className="m-0 flex flex-col gap-1 p-0" data-testid="steward-gates">
          {sortGatesFailingFirst(data.gates).map((g) => (
            <GateLine key={g.id} gate={g} />
          ))}
        </ul>
      </div>
      {data.findings.length > 0 && (
        <div className="flex flex-col gap-2" data-testid="steward-findings">
          <h3 className="m-0 text-ui-sm font-medium">Findings</h3>
          {data.findings.some((f) => f.axis) ? (
            groupFindingsByAxisThenFile(data.findings).map((g) => (
              <div key={g.axis ?? ""} className="flex flex-col gap-2" data-testid={g.axis ? `steward-findings-${g.axis}` : undefined}>
                {g.axis && <h4 className="m-0 text-ui-xs font-medium text-foreground-muted">{axisLabel(g.axis)}</h4>}
                <FileBlocks files={g.files} />
              </div>
            ))
          ) : (
            <FileBlocks files={groupFindingsByFile(data.findings)} />
          )}
        </div>
      )}
      {data.frames && data.frames.length > 0 && (
        <div className="flex flex-col gap-1.5" data-testid="steward-frames">
          <h3 className="m-0 text-ui-sm font-medium">Evidence frames</h3>
          <div className="grid grid-cols-2 gap-2">
            {data.frames.map((f) => (
              <FrameThumb key={f.name} reviewId={data.id} frame={f} />
            ))}
          </div>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1">
        {data.findings.length > 0 && (
          <Button type="button" variant="outline" size="sm" className={btn} data-testid={`steward-copy-${item.key}`} onClick={copyFindings}>
            <Icons.copy aria-hidden className="size-3.5" />
            {copied ? "Copied" : "Copy findings"}
          </Button>
        )}
        {data.prNumber !== undefined && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={btn}
            data-testid={`steward-open-pr-${item.key}`}
            onClick={() => (prUrl ? openExternalUrl(prUrl) : onOpenTask())}
          >
            <Icons.externalLink aria-hidden className="size-3.5" />
            Open PR
          </Button>
        )}
        <span role="status" aria-live="polite" className="sr-only" data-testid="steward-live">
          {copied ? "Findings copied" : ""}
        </span>
      </div>
    </div>
  );
}
