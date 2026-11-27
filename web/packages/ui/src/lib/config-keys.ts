/**
 * The global config keys the UI may switch (ADR-0062 section 3): the writable subset of the daemon's allowlist
 * (internal/config/allowlist.go). Every other key, browser.disabled included, is read-only and never gets a switch.
 */
export const WRITABLE_CONFIG_KEYS = [
  "automations.enabled",
  "automations.paused",
  "plugins.enabled",
  "fleet.enabled",
  "learning.enabled",
] as const;

export type WritableConfigKey = (typeof WRITABLE_CONFIG_KEYS)[number];

export function isWritableConfigKey(k: string): k is WritableConfigKey {
  return (WRITABLE_CONFIG_KEYS as readonly string[]).includes(k);
}

/** What the resume confirm says (the daemon table's sentence for automations.paused describes pausing, which needs no confirm). */
export const RESUME_CONFIRM = "Resuming lets the engine fire again: it polls schedules and starts runs without you asking.";

/** The one sentence shown before a key is turned on (or resumed), copied from the daemon's table; a test keeps them equal. */
export const CONFIG_CONFIRM: Record<WritableConfigKey, string> = {
  "automations.enabled": "Turning automations on arms the engine: it polls schedules and fires runs without you asking.",
  "automations.paused": "Pausing the engine records every signal as paused and fires nothing until you resume.",
  "plugins.enabled": "Turning plugins on lets installed plugin code run inside the daemon; each plugin still needs its own consent.",
  "fleet.enabled": "Turning the fleet on opens links to peer hosts you have paired; it applies after the daemon restarts.",
  "learning.enabled": "Turning learning on lets sMind collect run evidence and distill skills in workspaces that opted in.",
};

/** One row of a `config.get` answer. All values are strings; `env` is a variable NAME, never its value. */
export type ConfigKeyView = {
  key: string;
  value: string;
  default: string;
  source: "default" | "file" | "env";
  restart: boolean;
  writable: boolean;
  env?: string;
};

export type ConfigGetResult = { keys: ConfigKeyView[] };
