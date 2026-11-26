/**
 * The wire shape of `profile.rolePack.check` / `profile.rolePack.apply`
 * (docs/plans/active/role-pack-sync.md, PR 2): the daemon decides which
 * fields differ and how; the web only renders what it answers.
 */

/** One seat field the pack may update: a prompt (carries its diff) or a capability (carries stored -> want). */
export interface RolePackField {
  name: string;
  kind: "prompt" | "capability";
  state: "current" | "stale" | "customised";
  /** A unified diff of the stored value against the pack's; empty when current. */
  diff: string;
  /** Capability fields only: the short stored and pack values. */
  stored?: string;
  want?: string;
}

/** One pack seat: `fields` is empty unless `status` is `ok`. */
export interface RolePackSeat {
  name: string;
  id: number;
  status: "ok" | "missing" | "owned";
  updatedAt: string;
  untouched: boolean;
  hasUpdates: boolean;
  fields: RolePackField[];
}

/** The read-only answer of `profile.rolePack.check`: every pack seat, in pack order. */
export interface RolePackCheck {
  packVersion: number;
  seatsWithUpdates: number;
  seats: RolePackSeat[];
}

/** One seat's outcome of a `profile.rolePack.apply`. */
export interface RolePackApplyResult {
  seat: string;
  id: number;
  fields: string[];
  outcome: "updated" | "changed_meanwhile" | "dry_run" | "failed";
  error?: string;
}

/** The answer of `profile.rolePack.apply`: where the backup went and what each seat did. */
export interface RolePackApplyView {
  backupDir: string;
  dryRun: boolean;
  results: RolePackApplyResult[];
}
