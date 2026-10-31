// Wire shapes of the fleet.host.* RPCs the mobile app consumes (ADR-0048 F1c wire contract). Mobile is a
// viewer with exactly one write: approving or rejecting a pending host. No grant is modelled here — a phone
// can only send the fixed read-only approval grant (api.ts); editing grants, pairing and reverse toggles
// stay on a computer (ADR-0048 S13).

export type FleetLinkState = 'active' | 'connecting' | 'down' | 'refused' | 'peer_disabled' | 'disabled' | 'revoked';

export interface FleetHost {
  hostId: string;
  alias: string;
  label: string;
  state: 'pending' | 'approved' | 'revoked';
  /** Responder side: this human must approve the pairing (the SAS comparison is on them). */
  pendingApproval: boolean;
  link: FleetLinkState;
  /** Refusal code, e.g. peer_identity_changed, fleet_version_unsupported. */
  linkCode?: string;
  linkGuidance?: string;
  outboundEnabled: boolean;
  inbound: { enabled: boolean };
  /** Six digits, present only while the pairing is pending. */
  sas?: string;
  /** RFC 3339. */
  createdAt: string;
}

/** `off` is true when the daemon answered method not found: fleet is off on it. */
export interface HostList {
  off: boolean;
  hosts: FleetHost[];
}
