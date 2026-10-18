import type { Connection } from "@/lib/platform";

// ADR-0033: whether an OAuth login's browser half can open on this device
// ("this-device") or must be handed off to another device (remote).

export type LoginLocationOverride = "this-device" | "other-device";

// Loopback hosts: a browser opened on THIS device can be redirected back
// to the daemon's OAuth listener, so the sign-in isn't remote.
export function isLoopbackHostname(hostname: string): boolean {
  const host = hostname.toLowerCase();
  if (host === "localhost" || host === "::1" || host === "[::1]") return true;
  if (host.endsWith(".localhost")) return true;
  if (/^127\.\d+\.\d+\.\d+$/.test(host)) return true;
  return false;
}

export function isRemoteLogin(input: {
  connection: Pick<Connection, "kind"> | null;
  hostname: string;
  override: LoginLocationOverride | null;
}): boolean {
  if (input.override === "other-device") return true;
  if (input.override === "this-device") return false;
  if (input.connection) {
    return input.connection.kind !== "local";
  }
  return !isLoopbackHostname(input.hostname);
}
