/**
 * The `relay.device.*` wire (internal/wsapi/relay_devices.go): the devices paired through the relay and the pairing
 * window. Local-only RPCs: over a relay session every one of them is refused ("forbidden"), and without the relay
 * bridge the method does not exist ("method not found").
 */

/** The device's public key as the wire carries it (string or number[]). Never rendered, never logged. */
export type RelayDevicePub = unknown;

export type RelayDeviceState = "pending" | "approved" | "revoked";

export interface RelayDevice {
  device_id: string;
  device_pub: RelayDevicePub;
  label: string;
  first_seen: string;
  last_seen: string;
  state: RelayDeviceState;
  connected: boolean;
  /** Whether the device is registered for push; absent when it is not known. */
  push?: boolean;
}

export interface RelayDeviceWindow {
  expires_at: string;
  remaining: number;
}

export interface RelayDeviceList {
  devices: RelayDevice[];
  window: RelayDeviceWindow | null;
}

/** The first 8 characters of a device id: the only device identifier the UI ever shows. */
export function shortDeviceId(id: string): string {
  return id.slice(0, 8);
}

/** The label as a person reads it, or "Unnamed device" when it is empty or whitespace. */
export function deviceName(d: Pick<RelayDevice, "label">): string {
  const label = d.label.trim();
  return label === "" ? "Unnamed device" : label;
}

/**
 * Why the device list is unavailable: "relay" when the daemon refused the local-only call (this connection is a relay
 * session), "no-bridge" when the method does not exist (the daemon runs no relay bridge), null for any other error.
 */
export function relayDeviceUnavailable(err: unknown): "relay" | "no-bridge" | null {
  const code = (err as { code?: string } | null | undefined)?.code;
  if (code === "forbidden") return "relay";
  const message = err instanceof Error ? err.message : String(err);
  if (/method not found/i.test(message)) return "no-bridge";
  return null;
}
