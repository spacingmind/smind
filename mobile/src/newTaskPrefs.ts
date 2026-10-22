// newTaskPrefs.ts remembers the last new-task selection for the session
// only (module state, like Paseo's preferences.ts shape); persistence
// across launches is a follow-up, so no storage dependency lives here.

import { NewTaskSelection, RememberedSelection } from './providerCatalog';

let state: RememberedSelection = {};

/** Records provider + that provider's model/mode as the last used selection. */
export function rememberSelection(sel: NewTaskSelection): void {
  state = { ...state, provider: sel.provider, byProvider: { ...state.byProvider, [sel.provider]: { model: sel.model, mode: sel.mode } } };
}

/** The last remembered selection (empty object at session start). */
export function recallSelection(): RememberedSelection {
  return state;
}

/** Resets to a fresh session; for tests. */
export function resetNewTaskPrefs(): void {
  state = {};
}
