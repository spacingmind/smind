// stopControl.ts is the pure view-model for the task screen's Stop control
// (a port of web's composer handleStop / stop button, minus the react
// state): stopControlView derives what the button renders from the live
// run + stopping flag, and requestStop performs the stop, returning the
// error message instead of throwing so the screen can show it as the form
// error like web does.

import { RelayConnection } from './relay/RelayConnection';
import { stopRun } from './api';

export interface StopControlView {
  visible: boolean;
  label: 'Stop' | 'Stopping…';
  disabled: boolean;
}

export function stopControlView(liveRunId: string | null, stopping: boolean): StopControlView {
  if (liveRunId === null) return { visible: false, label: 'Stop', disabled: false };
  return stopping ? { visible: true, label: 'Stopping…', disabled: true } : { visible: true, label: 'Stop', disabled: false };
}

export async function requestStop(conn: RelayConnection, runId: string): Promise<string | null> {
  try {
    await stopRun(conn, runId);
    return null;
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
}
