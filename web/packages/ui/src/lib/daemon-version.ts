import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/**
 * Version skew between the app and the daemon it is connected to
 * (ADR-0031 P10).
 *
 * `compareVersions` mirrors the Rust `daemon_manager::version::compare` (the
 * desktop probe's rule) so a daemon version read over `/ws` (`daemon.info`,
 * the only channel a relay connection has) is judged exactly like one read
 * from `/healthz`: only two clean releases are ever ordered; a dev build or
 * anything unparseable is "unknown", which is the "never nag" rule -- a
 * `0.12.0-dev+abc1234` daemon next to a packaged app never shows the notice.
 *
 * Paseo does the same on the client, over the existing connection:
 * `isVersionMismatch(appVersion, daemonVersion)` in
 * packages/app/src/desktop/updates/desktop-updates.ts:168 reads the daemon's
 * version from `serverInfo` (the WS handshake) and gates its "update the
 * daemon" card in screens/settings/host-page.tsx:683-694,807 on it. Ours
 * orders versions (Paseo only checks inequality) because an app that is
 * *older* than its daemon must not tell the user to update the daemon.
 */

export type VersionComparison = "older" | "same" | "newer" | "unknown";

/** [major, minor, patch] of a clean release, or null for a dev/unknown version. */
function parseRelease(raw: string): [number, number, number] | null {
  const trimmed = raw.trim();
  const stripped = trimmed.startsWith("v") ? trimmed.slice(1) : trimmed;
  if (stripped === "dev" || stripped.includes("-dev")) return null;
  const parts = stripped.split(".");
  if (parts.length !== 3 || !parts.every((p) => /^\d+$/.test(p))) return null;
  return [Number(parts[0]), Number(parts[1]), Number(parts[2])];
}

/** Orders `daemon` against `app` from the daemon's point of view: "older" means the daemon is older than the app. */
export function compareVersions(app: string, daemon: string): VersionComparison {
  const a = parseRelease(app);
  const d = parseRelease(daemon);
  if (!a || !d) return "unknown";
  for (let i = 0; i < 3; i++) {
    if (d[i] < a[i]) return "older";
    if (d[i] > a[i]) return "newer";
  }
  return "same";
}

/** The first release that ships `smind update` (ADR-0031 P6, v0.12.0); an older daemon has to be reinstalled instead. */
export const UPDATE_COMMAND_MIN_VERSION = "0.12.0";

export interface UpdateCommand {
  /** Which machine the command is for ("Linux / macOS"), or null when there is only one. */
  label: string | null;
  command: string;
}

/**
 * The commands that bring an unmanaged daemon up to date: `smind update` when
 * the daemon already has it, otherwise the installer one-liners (which are
 * also the update path -- re-running them installs over the previous version).
 */
export function updateCommands(daemonVersion: string | null): UpdateCommand[] {
  const vsFirst = daemonVersion === null ? "unknown" : compareVersions(UPDATE_COMMAND_MIN_VERSION, daemonVersion);
  if (vsFirst === "same" || vsFirst === "newer") return [{ label: null, command: "smind update" }];
  return [
    { label: "Linux / macOS", command: "curl -fsSL https://spacingmind.com/install.sh | sh" },
    { label: "Windows (PowerShell)", command: "irm https://spacingmind.com/install.ps1 | iex" },
  ];
}

/** A `/healthz` or `daemon.info` `managedBy` value as a person would say it. */
export function managedByLabel(managedBy: string): string {
  // `taskscheduler` is what the Windows scheduled task sets (ADR-0031 P9); `task` is the value the ADR first sketched.
  return managedBy === "taskscheduler" || managedBy === "task" ? "Task Scheduler" : managedBy;
}

function isDismissedMap(value: unknown): value is Record<string, string> {
  return typeof value === "object" && value !== null && !Array.isArray(value) && Object.values(value).every((v) => typeof v === "string");
}

/** The token a dismissal is keyed to: a new daemon or app version is a new notice. */
export function noticeToken(daemonVersion: string | null, appVersion: string): string {
  return `${daemonVersion ?? "unknown"}->${appVersion}`;
}

export function isNoticeDismissed(connectionId: string, token: string): boolean {
  return readStored(STORAGE_KEYS.dismissedDaemonNotices, {}, isDismissedMap)[connectionId] === token;
}

export function dismissNotice(connectionId: string, token: string): void {
  const all = readStored(STORAGE_KEYS.dismissedDaemonNotices, {}, isDismissedMap);
  writeStored(STORAGE_KEYS.dismissedDaemonNotices, { ...all, [connectionId]: token });
}
