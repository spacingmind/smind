// firstSend.ts is M6-B2's pure provider resolution for a task with no
// run: the latest run's provider (follow-up path) is moot here, but the
// function also covers the general shape so the screen has one entry
// point. Kept pure -- no react-native import -- per providerCatalog's
// discipline.

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
