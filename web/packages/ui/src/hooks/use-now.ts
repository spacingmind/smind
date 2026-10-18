import { useEffect, useState } from "react";

/** The current time, re-read every `intervalMs`: derived state that expires (a cooldown) re-renders without any other event. */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs]);
  return now;
}
