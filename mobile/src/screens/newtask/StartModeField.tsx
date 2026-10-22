// StartModeField is the ADR-0039 G4 slot: where "Branch / PR / issue"
// start-mode inputs will land. HEAD (today's only StartMode) needs no
// input, so the slot renders nothing -- adding a variant is a new
// component here, not a rewrite of NewTaskScreen.

import { StartMode } from '../../api';

export function StartModeField({ start, onChange }: { start: StartMode; onChange: (start: StartMode) => void }) {
  void start;
  void onChange;
  return null;
}
