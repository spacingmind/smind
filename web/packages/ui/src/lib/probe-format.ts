import type { ProbeArmRole, ProbeArmStatus, ProbeMetricKey, ProbeStatus, ProbeVerdict } from "@/lib/probe-types";
import type { StatusBadgeStatus } from "@/components/ui/status-badge";

/** Millisecond durations as "1m 12s", "58s", "1h 02m"; under a second "<1s", zero "0s". */
export function formatDurationMs(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "0s";
  if (ms < 1000) return "<1s";
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  if (m > 0) return `${m}m ${String(s).padStart(2, "0")}s`;
  return `${s}s`;
}

/** Spent USD with four decimals under 1 and two from 1 up: "$0.4100", "$1.21". */
export function formatCostUSD(usd: number): string {
  const n = Number.isFinite(usd) ? Math.max(0, usd) : 0;
  return n < 1 ? `$${n.toFixed(4)}` : `$${n.toFixed(2)}`;
}

/** The rounded estimate in the button: "USD 1.4", "USD 0.96". */
export function formatEstimateUSD(usd: number): string {
  const n = Number.isFinite(usd) ? Math.max(0, usd) : 0;
  return `USD ${n < 1 ? n.toFixed(2) : n.toFixed(1)}`;
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

export const METRIC_LABELS: Record<ProbeMetricKey, string> = {
  toolCalls: "Tool calls",
  failedToolCalls: "Failed tool calls",
  permissionDenials: "Permission denials",
  turns: "Turns",
  costUSD: "Cost",
  durationMs: "Duration",
  diffBytes: "Diff size",
};

/** One metric value in its own unit. */
export function formatMetric(key: ProbeMetricKey, value: number): string {
  switch (key) {
    case "costUSD":
      return formatCostUSD(value);
    case "durationMs":
      return formatDurationMs(value);
    case "diffBytes":
      return formatBytes(value);
    default:
      return String(Math.round(value));
  }
}

/** A signed difference: "−6", "+0.0100"; a zero difference has no sign. */
export function formatDelta(key: ProbeMetricKey, delta: number): string {
  if (!Number.isFinite(delta) || delta === 0) return formatMetric(key, 0);
  const sign = delta < 0 ? "−" : "+";
  return `${sign}${formatMetric(key, Math.abs(delta))}`;
}

export const VERDICT_LABELS: Record<ProbeVerdict, string> = {
  helped: "Helped (consistent)",
  no_effect: "No observed effect",
  never_loaded: "Never loaded",
  hurt: "Hurt / regressed",
  inconclusive: "Inconclusive",
  probe_failed: "Probe failed (infra)",
  unsafe: "Unsafe (tripwire)",
};

/** Never `success`: a probe verdict is evidence for the review, not an approval. */
export const VERDICT_TONES: Record<ProbeVerdict, StatusBadgeStatus> = {
  helped: "neutral",
  no_effect: "neutral",
  never_loaded: "neutral",
  hurt: "warning",
  inconclusive: "neutral",
  probe_failed: "neutral",
  unsafe: "danger",
};

export const ARM_LABELS: Record<ProbeArmRole, string> = {
  A: "Control",
  A2: "Control replicate",
  B: "With this skill",
};

const ARM_STATUS_LABELS: Record<ProbeArmStatus, string> = {
  pending: "Waiting",
  running: "Running",
  done: "Done",
  capped: "Stopped: cost cap",
  timeout: "Stopped: timed out",
  turns: "Stopped: turn limit",
  output: "Stopped: output limit",
  killed: "Stopped",
  error: "Error",
};

export function armStatusLabel(status: ProbeArmStatus): string {
  return ARM_STATUS_LABELS[status];
}

/** Queued or running: the probe is in flight. */
export function isProbeActive(status: ProbeStatus | undefined): boolean {
  return status === "queued" || status === "running";
}
