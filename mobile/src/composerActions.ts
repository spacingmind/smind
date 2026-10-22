// composerActions.ts is the pure view-model for the composer toolbar (M5):
// which of Send now / Stop / Send show, their label and disabled state.
// Built on stopControlView so the Stop rules stay in one place.

import { stopControlView, StopControlView } from './stopControl';

export interface ComposerActions {
  sendNow: { visible: boolean };
  stop: { visible: boolean; label: StopControlView['label']; disabled: boolean };
  send: { disabled: boolean };
}

export function composerActions({ liveRunId, canSend, stopping }: { liveRunId: string | null; canSend: boolean; stopping: boolean }): ComposerActions {
  const stop = stopControlView(liveRunId, stopping);
  return {
    sendNow: { visible: liveRunId !== null && canSend },
    stop: { visible: stop.visible, label: stop.label, disabled: stop.disabled },
    send: { disabled: !canSend },
  };
}
