import type { ConnectMethodId } from "@/components/first-run/connect-methods";
import type { DaemonStatus } from "@/lib/platform";

export type ChoiceId = "run" | "existing" | ConnectMethodId;

/** What first-run learned about this computer from `daemon_status` (ADR-0031 D7 pre-selection rules). */
export interface LocalSituation {
  /** `daemon_status` answered; false = unknown (nothing pre-selected, Run here still offered). */
  known: boolean;
  /** Linux deb: the app cannot manage a daemon here, (a) is replaced by guidance. */
  unsupported: boolean;
  /** A running daemon this app already manages (re-running setup): Run here keeps it instead of reinstalling. */
  alreadyManaged: boolean;
  /** (d) is offered: a smind daemon answers and the app does not manage it. */
  existing: { text: string } | null;
  /** Why (a) is disabled; null = enabled. */
  runBlocked: string | null;
  preselect: ChoiceId | null;
  /** Windows: the answering daemon may be running inside WSL. */
  wsl: boolean;
}

const PORT_NOTE = "The app does not pick another port; stop that program or free the port, then run setup again.";

export function describeLocalSituation(status: DaemonStatus | null): LocalSituation {
  if (!status) {
    return { known: false, unsupported: false, alreadyManaged: false, existing: null, runBlocked: null, preselect: null, wsl: false };
  }
  const unsupported = status.platform === "unsupported";
  const smind = status.localService === "smind";
  const wsl = status.platform === "windows" && status.wslDaemonSuspected;
  const alreadyManaged = status.reachable && smind && status.managedState === "managed";
  const base = { known: true, unsupported, alreadyManaged, wsl };

  if (status.reachable && smind && status.managedState !== "managed") {
    const version = status.daemonVersion ? `smind v${status.daemonVersion.replace(/^v/, "")}` : "A smind daemon";
    const where = wsl ? "started outside the app (it looks like a daemon running inside WSL)" : "started outside the app";
    return {
      ...base,
      existing: { text: `${version} is already running on this computer, ${where}. The app will connect to it and leave it alone.` },
      runBlocked: "A smind daemon is already running here, so the app won't install a second one.",
      preselect: "existing",
    };
  }
  if (status.reachable && !smind) {
    return {
      ...base,
      existing: null,
      runBlocked: `Something that is not smind answers on this computer's smind port (127.0.0.1:4648). ${PORT_NOTE}`,
      preselect: null,
    };
  }
  if (status.portOccupied && !status.reachable) {
    return {
      ...base,
      existing: null,
      runBlocked: `Another program is using this computer's smind port (127.0.0.1:4648) without answering as smind. ${PORT_NOTE}`,
      preselect: null,
    };
  }
  return { ...base, existing: null, runBlocked: null, preselect: unsupported ? null : "run" };
}
