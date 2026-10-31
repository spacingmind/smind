import { useCallback, useEffect, useRef, useState } from "react";

import {
  canBack,
  canForward,
  dropEntry,
  emptyHistory,
  pushEntry,
  sameEntry,
  type NavEntry,
} from "@/lib/nav-history";

/**
 * Drives the in-app back/forward history over the live navigation state.
 *
 * `current` is where the app is right now (view + selected task) and
 * `apply` tries to take it somewhere else, reporting false when the target
 * no longer exists -- a step onto a gone entry drops it from the history
 * and keeps walking. Selections the hook itself did not cause are recorded;
 * ones that came from `back`/`forward` are not, so a step never lands in
 * the history twice.
 */
export function useNavHistory(
  current: NavEntry,
  apply: (entry: NavEntry) => boolean | Promise<boolean>,
): {
  canBack: boolean;
  canForward: boolean;
  back: () => Promise<void>;
  forward: () => Promise<void>;
} {
  const historyRef = useRef(emptyHistory());
  const [, bump] = useState(0);
  const applyRef = useRef(apply);
  applyRef.current = apply;
  // The entry our own in-flight step asked for: seeing it arrive in
  // `current` is the step landing, not a new selection to record.
  const pendingStepRef = useRef<NavEntry | null>(null);

  useEffect(() => {
    const pending = pendingStepRef.current;
    if (pending && sameEntry(pending, current)) {
      pendingStepRef.current = null;
      return;
    }
    const history = historyRef.current;
    const cursor = history.entries[history.index];
    if (!cursor || !sameEntry(cursor, current)) {
      historyRef.current = pushEntry(history, current);
      bump((v) => v + 1);
    }
  }, [current.view, current.taskId]);

  const step = useCallback(async (direction: -1 | 1): Promise<void> => {
    for (;;) {
      const history = historyRef.current;
      const i = history.index + direction;
      if (i < 0 || i >= history.entries.length) return;
      const target = history.entries[i];
      pendingStepRef.current = target;
      // The cursor moves only once apply confirms the target still exists;
      // a false drops the entry and leaves the next candidate one further
      // in the same direction (below the cursor the removal shifts it up).
      if (await applyRef.current(target)) {
        historyRef.current = { entries: history.entries, index: i };
        bump((v) => v + 1);
        return;
      }
      pendingStepRef.current = null;
      historyRef.current = dropEntry(history, i);
      bump((v) => v + 1);
    }
  }, []);

  const back = useCallback(() => step(-1), [step]);
  const forward = useCallback(() => step(1), [step]);

  const history = historyRef.current;
  return { canBack: canBack(history), canForward: canForward(history), back, forward };
}
