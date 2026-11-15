import { useEffect, useState } from "react";

/**
 * Re-renders every `intervalMs` while mounted (null = off): the shared
 * tick behind every label that ages (a running run's elapsed time, a
 * finished run's relative age, a rate-limit reset).
 */
export function useTicker(intervalMs: number | null): void {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (intervalMs === null) return;
    const id = setInterval(() => setTick((n) => n + 1), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
}
