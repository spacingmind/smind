import type { RolePackApplyResult, RolePackField } from "@/lib/role-pack-types";

/**
 * Every user-facing sentence of role-pack sync (PR 2), pure so a test can pin
 * each one. The product is "sMind"; the profiles are "your workspace"'s.
 */

/** The banner above the profile list: `Role prompts have updates for 1 seat` / `... for N seats`. */
export function bannerText(seats: number): string {
  return `Role prompts have updates for ${seats} ${seats === 1 ? "seat" : "seats"}`;
}

/** The quiet line when nothing drifted: `Role prompts match pack v9`. */
export function currentLine(packVersion: number): string {
  return `Role prompts match pack v${packVersion}`;
}

const FIELD_LABELS: Record<string, string> = {
  instructions: "Instructions",
  notes: "Notes",
  skills: "Skills",
  model: "Model",
  permissionMode: "Permission mode",
  orchestrationTools: "Orchestration tools",
  childPolicy: "Child policy",
  onRateLimit: "On rate limit",
};

/** A synced field's label; an unknown name (a newer pack) shows as itself, never blank. */
export function fieldLabel(name: string): string {
  return FIELD_LABELS[name] ?? name;
}

/** What a field's state reads as in the review dialog. */
export function stateLabel(state: RolePackField["state"]): string {
  if (state === "stale") return "Update available";
  if (state === "customised") return "Edited by you";
  return "Current";
}

/** One result line of an apply, as the dialog shows it. */
export function outcomeText(r: RolePackApplyResult): string {
  const fields = r.fields.join(", ");
  switch (r.outcome) {
    case "updated":
      return `Updated ${fields}`;
    case "changed_meanwhile":
      return `${r.seat} changed since the check; the list was refreshed, review it again`;
    case "failed":
      return `Failed: ${r.error ?? ""}`;
    case "dry_run":
      return `Would update ${fields}`;
  }
}
