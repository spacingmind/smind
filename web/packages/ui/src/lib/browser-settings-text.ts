/**
 * The residual statements shown next to the browser switch (ADR-0061, ADR-0056). They mirror
 * `public-docs/browser.md`; `browser-section.test.tsx` reads that page and asserts each key phrase is in both,
 * so the two cannot drift apart silently.
 */

/** (a) The unauthenticated-local limit. */
export const BROWSER_RESIDUAL_LOCAL =
  "A local program that talks to the daemon without saying who it is counts as you, and so does anything running inside the agent's own process tree. Keep the daemon token private.";
export const BROWSER_RESIDUAL_LOCAL_KEY = "talks to the daemon without saying who it is counts as you";

/** (b) The ADR-0056 shell limit. */
export const BROWSER_RESIDUAL_SHELL =
  "The permission cards protect against the model, not against a shell-capable agent that attacks the daemon: it could answer a card through the daemon's websocket. Do not let an agent you do not trust run shell commands.";
export const BROWSER_RESIDUAL_SHELL_KEY = "protect against the model, not against a shell-capable agent";

/** (c) The Windows statement; the per-provider statements come from the daemon's reach table. */
export const BROWSER_RESIDUAL_WINDOWS = "On Windows the browser is not offered.";
export const BROWSER_RESIDUAL_WINDOWS_KEY = "On Windows the browser is not offered";

/**
 * The daemon words a refusal as "<rpc method>: <package>: <sentence>" (e.g. `browser.settings.set: browser: allowlist
 * entry "*.x" must be one exact host...`). The person reads a sentence: the leading method and package labels are
 * internal and are dropped, the first letter is capitalised and a full stop is added when the text has none. The words
 * in between are the daemon's, unchanged (the CLI keeps printing the daemon's text as it is).
 */
export { daemonSentence } from "@/lib/daemon-sentence";
