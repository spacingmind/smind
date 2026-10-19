import { useCallback, useState } from "react";

/**
 * The pending/error plumbing every confirm dialog repeated by hand: `run(fn)` flips `pending`, clears the
 * last error, awaits `fn`, and keeps the daemon's message (verbatim) when it throws. Resolves `true` on
 * success so the caller can close the dialog and run its follow-up, `false` on failure (the dialog stays
 * open with `error` visible). `reset()` clears a stale error (e.g. when the dialog reopens).
 */
export function useConfirmAction(): {
  pending: boolean;
  error: string | null;
  run: (action: () => Promise<unknown>) => Promise<boolean>;
  reset: () => void;
} {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = useCallback(async (action: () => Promise<unknown>): Promise<boolean> => {
    setPending(true);
    setError(null);
    try {
      await action();
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setPending(false);
    }
  }, []);
  const reset = useCallback(() => setError(null), []);
  return { pending, error, run, reset };
}
