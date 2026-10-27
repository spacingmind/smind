// pause.ts: the Pause brake, the one mutation the phone offers. It asks first (confirm), then calls
// automation.disable exactly once; a daemon error comes back as text for the screen to show. Nothing else here may
// call the daemon: automationsReadOnly.test.ts walks this module too.

export type PauseResult = { kind: 'cancelled' } | { kind: 'paused' } | { kind: 'error'; message: string };

export const PAUSE_TITLE = 'Pause this automation?';
export function pauseMessage(name: string): string {
  return `${name} will stop firing until it is turned back on from the computer.`;
}

export async function pauseWithConfirm(opts: {
  name: string;
  /** The native alert/sheet: resolves true only when the person confirmed. */
  confirm: (title: string, message: string) => Promise<boolean>;
  disable: () => Promise<void>;
}): Promise<PauseResult> {
  if (!(await opts.confirm(PAUSE_TITLE, pauseMessage(opts.name)))) return { kind: 'cancelled' };
  try {
    await opts.disable();
    return { kind: 'paused' };
  } catch (e) {
    return { kind: 'error', message: e instanceof Error ? e.message : String(e) };
  }
}
