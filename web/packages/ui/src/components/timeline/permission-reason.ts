import type { StatusBadgeStatus } from "@/components/ui/status-badge";
import type { PermissionResolutionReason } from "@/lib/types";

/**
 * How a resolved permission's `reason` (ADR 0008 / task-permission-ux.md
 * Item 2) reads inline: a short label plus which `StatusBadge` tone it
 * gets. `human` reads as the calm, expected case (a person decided);
 * `auto_accept` as informational (the run's own Auto-accept setting
 * decided, not a person -- ADR-0019); `policy_allow` / `policy_deny` the
 * same informational tier (the daemon's human-authored policy engine
 * resolved the request, ADR-0047 -- a standing decision, still not a
 * person); `auto_safe` is legacy (smind's removed allowlist) and only
 * ever comes from an older daemon; `timeout` and
 * `provider_cancellation` as worth a second look (nobody decided in
 * time, or the request vanished out from under the decider before anyone
 * could -- see
 * docs/plans/active/claude-native-permission-cancellation.md). A reason
 * this build has never heard of has no entry here, and
 * `permissionResolutionLabel` renders it as its raw text in the neutral
 * tone; a payload with no reason at all (an older server) is the only
 * case left with no badge.
 */
export const PERMISSION_REASON_LABEL: Partial<Record<PermissionResolutionReason, { label: string; status: StatusBadgeStatus }>> = {
  human: { label: "You approved", status: "success" },
  auto_accept: { label: "Auto-accepted", status: "running" },
  auto_safe: { label: "Auto-approved (legacy)", status: "running" },
  policy_allow: { label: "Allowed by policy", status: "running" },
  policy_deny: { label: "Denied by policy", status: "warning" },
  timeout: { label: "Timed out", status: "warning" },
  provider_cancellation: { label: "Cancelled by provider", status: "warning" },
};

/** The kind prefix ACP and claude-native use for a refusal ("reject_once", "reject_always"). */
function isRejection(optionKind: string | undefined): boolean {
  return optionKind !== undefined && optionKind.startsWith("reject");
}

/**
 * The label for a resolved permission row. Same as PERMISSION_REASON_LABEL, except that a person who
 * chose a reject option reads "You denied", not "You approved" (reason `human` only says a person decided).
 * A reason this build has never heard of renders as its raw text in the neutral tone, never a crash;
 * only a missing reason (an older server payload) returns undefined -- no badge.
 */
export function permissionResolutionLabel(item: {
  reason?: PermissionResolutionReason;
  optionKind?: string;
}): { label: string; status: StatusBadgeStatus } | undefined {
  if (!item.reason) return undefined;
  if (item.reason === "human" && isRejection(item.optionKind)) return { label: "You denied", status: "warning" };
  return PERMISSION_REASON_LABEL[item.reason] ?? { label: item.reason, status: "neutral" };
}
