// firstSend.ts resolves the provider a task's first prompt sends with
// when the task has no run. Kept pure -- no react-native import -- per
// providerCatalog's discipline.

import { ProviderInfo } from './api';
import { defaultSelection, RememberedSelection } from './providerCatalog';

/**
 * The provider a first prompt sends with: a latest run's provider wins
 * (even if the catalog no longer lists it -- it is a fact about the
 * task, not a preference), else the new-task default selection
 * (remembered else first provider), else null for an empty catalog.
 */
export function firstSendProvider(
  providers: ProviderInfo[],
  runProvider: string | null,
  remembered: RememberedSelection,
): string | null {
  if (runProvider !== null) return runProvider;
  return defaultSelection(providers, remembered)?.provider ?? null;
}
