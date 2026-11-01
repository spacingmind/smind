/** Wire shapes of the `fleet.host.*` RPCs (ADR-0048). */

export interface FleetGrant {
  ops: string[];
  workspacesRead: { all?: boolean; ids?: number[] };
  workspacesWrite: number[];
  providers: string[];
  maxLiveAgents: number;
  maxWallSeconds: number;
  maxCostUsd: number;
  permissionMode?: string;
}

export type FleetHostState = "pending" | "approved" | "revoked";

export type FleetLinkState = "active" | "connecting" | "down" | "refused" | "peer_disabled" | "disabled" | "revoked";

export interface FleetHost {
  hostId: string;
  alias: string;
  label: string;
  /** Operator routing labels (caller-local, at most 16); absent on a daemon that predates them. */
  labels?: Record<string, string>;
  state: FleetHostState;
  pendingApproval: boolean;
  link: FleetLinkState;
  linkCode?: string;
  linkGuidance?: string;
  outboundEnabled: boolean;
  inbound: { enabled: boolean; grant: FleetGrant };
  sas?: string;
  createdAt: string;
  lastSeenAt?: string;
  version?: string;
  os?: string;
  arch?: string;
}

/** One fleet exchange from `fleet.host.audit`: a hash and a masked, redacted prefix of the request, never its text. */
export interface FleetAuditEntry {
  id: number;
  time: string;
  direction: "in" | "out";
  hostId: string;
  peer: string;
  method: string;
  /** "ok", "refused" or "error" (the wall limit stamps "wall_limit"). */
  decision: string;
  code?: string;
  summary: string;
  latencyMs: number;
}

/** `off` is true when the daemon has fleet disabled (its `fleet.host.*` methods are not registered). */
export interface FleetHostList {
  off: boolean;
  hosts: FleetHost[];
}

/** The reverse direction starts read only; the copy shown wherever it is switched on. */
export const REVERSE_GRANT_COPY =
  "Reverse access is read only: this host can list your workspaces and tasks and see run status, but it cannot create, send or stop anything (0 live agents, 0 s of run time, $0). You can widen it later.";

/** True when the daemon answered "method not found": fleet is off on it. */
export function isFleetOff(err: unknown): boolean {
  return err instanceof Error && /method not found/i.test(err.message);
}

/** The human label of a link state. */
export function linkLabel(link: FleetLinkState): string {
  switch (link) {
    case "active":
      return "Connected";
    case "connecting":
      return "Connecting";
    case "down":
      return "Unreachable";
    case "refused":
      return "Refused";
    case "peer_disabled":
      return "Peer turned this direction off";
    case "disabled":
      return "Off";
    case "revoked":
      return "Revoked";
  }
}

/** The badge tone of a link state. */
export function linkTone(link: FleetLinkState): "success" | "warning" | "danger" | "neutral" {
  switch (link) {
    case "active":
      return "success";
    case "connecting":
    case "down":
      return "warning";
    case "refused":
    case "revoked":
      return "danger";
    case "peer_disabled":
    case "disabled":
      return "neutral";
  }
}

/** Drops the spaces a six-digit code is typed or shown with. */
export function normalizeSas(s: string): string {
  return s.replace(/\s+/g, "");
}

/** True for a six-digit short authentication string, spaces ignored. */
export function isValidSas(s: string): boolean {
  return /^\d{6}$/.test(normalizeSas(s));
}

/** True when the grant lets the peer create, send or stop but names no workspace it may write to. */
export function grantNeedsWriteWorkspace(g: FleetGrant): boolean {
  return g.ops.some((op) => op === "create" || op === "send" || op === "stop") && g.workspacesWrite.length === 0;
}
