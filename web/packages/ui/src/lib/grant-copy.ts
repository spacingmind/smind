import type { GrantOffer, SavedGrant } from "@/lib/types";

/**
 * Words for saved grants ("allow always in this project"), shared by the permission card and the
 * Saved grants list so the two never disagree. The daemon decides a grant's class; these only
 * name it.
 */

/** The control that saves the grant. */
export const GRANT_BUTTON_LABEL = "Always allow in this project";

/** Why the control is disabled for a runner until the opt-in is ticked. */
export const GRANT_NEEDS_OPT_IN_HINT = "Tick the box above to save this command.";

/** The opt-in's label when the daemon sends none (it always does today). */
export const GRANT_OPT_IN_FALLBACK = "Also allow running this project's code (the agent can change it)";

/** True when saving needs the project-code opt-in: the CLASS decides (anything but the known read-only class is a runner); the daemon's own flag cannot lower it, nor raise it for a read-only command (the daemon would refuse that opt-in). */
export function grantNeedsOptIn(offer: Pick<GrantOffer, "class">): boolean {
  return offer.class !== "readonly";
}

/** "Read-only" or "Runs project code": a text label for a class, never colour alone. */
export function grantClassLabel(klass: string): string {
  return klass === "readonly" ? "Read-only" : "Runs project code";
}

/** The status pill's words, with what each means for the next request. */
export function grantStatusLabel(status: SavedGrant["status"]): string {
  switch (status) {
    case "active":
      return "Active";
    case "expired":
      return "Expired";
    case "void":
      return "Void: tool setup changed";
    case "revoked":
      return "Revoked";
    default:
      return status;
  }
}

/** One line saying what a status means; empty for active. */
export function grantStatusHint(g: SavedGrant): string {
  switch (g.status) {
    case "expired":
      return "Not applied; this command asks again.";
    case "void":
      return "The project's MCP servers changed, so this grant is not applied; this command asks again.";
    case "revoked":
      return "Not applied; this command asks again.";
    default:
      return "";
  }
}

/** The scope sentence a revoke confirmation repeats: what will be asked again. */
export function grantRevokeCopy(g: SavedGrant): string {
  return `${g.command} will ask again in this project.`;
}
