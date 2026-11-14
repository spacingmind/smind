// Wire shapes of the saved-grant surface (docs/plans/active/saved-grants.md): the offer a permission request
// carries (internal/taskrunner GrantOffer) and a row of permission.grants.list. Every string is daemon-provided
// (the command is agent-authored text) and only ever drawn as plain <Text>.

/** The scope a permission card offers to save: this exact command, in this project, for this long. */
export interface GrantOffer {
  command: string;
  project: string;
  folder: string;
  expiresInDays: number;
  /** "readonly" inspects only; "runner" runs code the project defines. Anything else reads as a runner. */
  class: string;
  requiresOptIn: boolean;
  optInLabel?: string;
  note: string;
}

export interface SavedGrant {
  id: number;
  workspaceId: number;
  command: string;
  folder: string;
  class: string;
  runnerOptIn: boolean;
  createdAt: string;
  expiresAt: string;
  createdBy: string;
  revokedAt?: string;
  useCount: number;
  lastUsedAt?: string;
  /** active | expired | void | revoked; anything else is shown as given. */
  status: string;
}
