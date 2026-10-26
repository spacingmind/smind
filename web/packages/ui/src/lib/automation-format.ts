import type { AutomationAuditRow, AutomationSignalSpec, AutomationView } from "@/lib/automation-types";

/** "cron 0 3 * * * UTC" / "ci on develop": what starts the automation. */
export function signalSummary(signal: AutomationSignalSpec): string {
  if (signal.kind === "cron") return `cron ${signal.cron ?? ""} UTC`;
  return `${signal.workflow ?? ""} on ${signal.branch || "develop"}`;
}

export type AutomationState = "active" | "paused" | "disabled";

/** A paused automation (auto-paused after failures, or by the Human) reads as paused whether or not it is enabled. */
export function automationState(a: Pick<AutomationView, "enabled" | "status">): AutomationState {
  if (a.status === "paused") return "paused";
  return a.enabled ? "active" : "disabled";
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n: number) => String(n).padStart(2, "0");

/** "Oct 28 03:00 UTC". An unparseable value comes back as given. */
export function formatUtc(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())} UTC`;
}

/** The same instant in the viewer's zone: "Oct 28 10:00". */
export function formatLocal(value: Date | string): string {
  const d = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(d.getTime())) return String(value);
  return `${MONTHS[d.getMonth()]} ${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** What a manual fire answered, as one sentence: a decision other than "fire" is a refusal, never a success. */
export function fireOutcomeText(row: Pick<AutomationAuditRow, "decision" | "reason" | "taskId">): string {
  if (row.decision === "fire") return row.taskId ? `Started task #${row.taskId}.` : "Started a task.";
  return `Not started: ${row.decision}${row.reason ? ` (${row.reason})` : ""}.`;
}
