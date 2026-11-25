/**
 * The agent browser's per-workspace settings view (ADR-0061): the answer of the human-only, local-only
 * `browser.settings.get` / `browser.settings.set` RPCs (internal/wsapi/browser_settings.go). Everything the
 * Settings -> Browser section shows comes from this one view.
 */

/** Whether the browser row is handed to a provider's agent: yes, offered but not guaranteed, or no. */
export type BrowserReach = "yes" | "conditional" | "no";

/** One provider's browser reach (the daemon's reach table, internal/providers/browser_reach.go). */
export interface BrowserProviderReach {
  id: string;
  label: string;
  reach: BrowserReach;
  /** The daemon's one-sentence reason (said for "no" and "conditional"). */
  why?: string;
  /** Whether screenshots reach the agent as images: only "yes" does; "unknown" gets a text note. */
  images: "yes" | "no" | "unknown";
}

export interface BrowserSettingsView {
  workspaceId: number;
  enabled: boolean;
  /** Exact hosts the person added (loopback is always allowed and not listed). */
  allowHosts: string[];
  profileMode: string;
  /** The daemon's configuration disables the browser for every workspace. */
  killSwitch: boolean;
  /** The browser tools are offered to agents by this daemon version. */
  available: boolean;
  chromeInstalled: boolean;
  /** How to install a browser, said when none is installed. */
  installStep?: string;
  /** The daemon's rule for host names (public addresses only; an intranet host is added as its IP). */
  note: string;
  providers: BrowserProviderReach[];
  /** This workspace has a running browser session: an agent is using it now. */
  sessionLive: boolean;
  updatedAt?: string;
  updatedBy?: string;
}

/** The most hosts one workspace's allowlist may hold (the daemon's cap). */
export const BROWSER_ALLOW_MAX = 50;
