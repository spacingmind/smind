/**
 * A section can intercept the compact Settings header's back button: when
 * its own screen has an inner level (Providers: provider detail -> provider
 * list) the back pops that level first instead of leaving the section, so
 * the app shows exactly one back control.
 */
type BackGuard = () => boolean;

let guard: BackGuard | null = null;

/** Registers `next` (returns true when it handled the back); the returned function unregisters it if still current. */
export function setSettingsBackGuard(next: BackGuard): () => void {
  guard = next;
  return () => {
    if (guard === next) guard = null;
  };
}

/** True when a section handled the back press and Settings must stay where it is. */
export function runSettingsBackGuard(): boolean {
  return guard ? guard() : false;
}
