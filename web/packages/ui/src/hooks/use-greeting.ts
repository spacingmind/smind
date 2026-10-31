import { useEffect, useRef, useState } from "react";
import { greetingBucket, GREETINGS, OFFICE_GREETINGS, pickGreeting, type GreetingBucket } from "@/lib/greeting";
import type { InterfaceMode } from "@/lib/interface-mode";
import { useInterfaceModeStore } from "@/stores/interface-mode-store";

const TICK_MS = 60_000;

interface Held {
  bucket: GreetingBucket;
  mode: InterfaceMode;
  line: string;
}

function pick(bucket: GreetingBucket, mode: InterfaceMode, random: () => number): string {
  return pickGreeting(bucket, random, mode === "office" ? OFFICE_GREETINGS : GREETINGS);
}

/** A greeting for the local time of day; the line only changes when the bucket or the interface mode does. */
export function useGreeting(random: () => number = Math.random): string {
  const mode = useInterfaceModeStore((s) => s.value);
  const randomRef = useRef(random);
  randomRef.current = random;
  const modeRef = useRef(mode);
  modeRef.current = mode;
  const [held, setHeld] = useState<Held>(() => {
    const bucket = greetingBucket(new Date().getHours());
    return { bucket, mode, line: pick(bucket, mode, random) };
  });

  useEffect(() => {
    const refresh = () => {
      const bucket = greetingBucket(new Date().getHours());
      const current = modeRef.current;
      setHeld((prev) =>
        prev.bucket === bucket && prev.mode === current
          ? prev
          : { bucket, mode: current, line: pick(bucket, current, randomRef.current) },
      );
    };
    refresh();
    const id = setInterval(refresh, TICK_MS);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [mode]);

  return held.line;
}
