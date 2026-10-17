/**
 * The "open first-run again" request (ADR-0031 P5): Settings' "Run setup again" /
 * "Finish setup" button dispatches it, the first-run gate listens (together with the
 * native Help menu's `desktop.onRunSetupRequested`). A plain window event keeps the
 * settings sections free of any import of the gate itself.
 */
export const RUN_SETUP_EVENT = "smind:run-setup";

export function requestFirstRun(): void {
  if (typeof window !== "undefined") window.dispatchEvent(new Event(RUN_SETUP_EVENT));
}
