import { PERMISSION_REASON_LABEL } from "@/components/timeline/permission-reason";
import type { TimelineItem } from "@/hooks/use-run-timeline";

/** How each row kind is prefixed when a whole turn is copied as plain text. */
const COPY_PREFIX: Record<string, string> = {
  user: "> ",
  thinking: "[thinking] ",
};

/**
 * A whole run's transcript as plain text, for the turn footer's copy
 * action. Deliberately lossy and readable rather than a faithful dump of
 * the wire events: what a person pastes into an issue or a chat is the
 * conversation, with tool calls as one-line markers, not re-serialized
 * JSON arguments.
 */
export function timelineToText(prompt: string, items: TimelineItem[]): string {
  const lines = [`> ${prompt}`, ""];
  for (const item of items) {
    if (item.kind === "tool_call") {
      lines.push(`[tool] ${item.toolName ?? item.toolCallId}${item.title ? `: ${item.title}` : ""} (${item.status})`);
      continue;
    }
    if (item.kind === "unknown") {
      lines.push(`[unrecognised event: ${item.eventType}]`);
      continue;
    }
    if (item.kind === "permission") {
      const resolution = item.reason ? PERMISSION_REASON_LABEL[item.reason] : undefined;
      lines.push(`[permission resolved]${resolution ? ` (${resolution.label})` : ""}`);
      continue;
    }
    const prefix = COPY_PREFIX[item.kind] ?? "";
    lines.push(prefix ? item.text.split("\n").map((l) => prefix + l).join("\n") : item.text);
  }
  return lines.join("\n").trimEnd() + "\n";
}

/**
 * A turn's wall-clock duration, rendered short (`1.4s`, `2m 07s`).
 * `finishedAt` is optional so a still-running turn's footer counts up for
 * free: the run re-renders on every streamed chunk anyway, so passing
 * `Date.now()` as the end gives a live elapsed time with no timer of its
 * own (and it stops moving exactly when the output does).
 */
export function formatElapsed(startedAt: string, finishedAt?: string): string | null {
  const start = Date.parse(startedAt);
  if (Number.isNaN(start)) return null;
  const end = finishedAt ? Date.parse(finishedAt) : Date.now();
  if (Number.isNaN(end)) return null;
  const ms = Math.max(0, end - start);
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.floor((ms % 60_000) / 1000);
  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}
