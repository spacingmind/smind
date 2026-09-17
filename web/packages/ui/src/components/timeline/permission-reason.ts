import type { StatusBadgeStatus } from "@/components/ui/status-badge";
import type { PermissionResolutionReason } from "@/lib/types";

/**
 * How a resolved permission's `reason` (ADR 0008 / task-permission-ux.md
 * Item 2) reads inline: a short label plus which `StatusBadge` tone it
 * gets. `human` reads as the calm, expected case (a person decided);
 * `auto_safe` as informational (the policy decided, not a person);
 * `timeout` and `provider_cancellation` as worth a second look (nobody
 * decided in time, or the request vanished out from under the decider
 * before anyone could -- see
 * docs/plans/active/claude-native-permission-cancellation.md). A reason
 * this build has never heard of (or none at all, from an older server
 * payload) has no entry here -- callers treat a missing lookup as "no
 * badge", not a crash.
 */
export const PERMISSION_REASON_LABEL: Partial<Record<PermissionResolutionReason, { label: string; status: StatusBadgeStatus }>> = {
  human: { label: "You approved", status: "success" },
  auto_safe: { label: "Auto-approved", status: "running" },
  timeout: { label: "Timed out", status: "warning" },
  provider_cancellation: { label: "Cancelled by provider", status: "warning" },
};
