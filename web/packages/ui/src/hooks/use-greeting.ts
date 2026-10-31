import { useEffect, useRef, useState } from "react";
import { greetingBucket, pickGreeting, type GreetingBucket } from "@/lib/greeting";

const TICK_MS = 60_000;

interface Held {
  bucket: GreetingBucket;
  line: string;
}

/** A greeting for the local time of day; the line only changes when the bucket does. */
export function useGreeting(random: () => number = Math.random): string {
  const randomRef = useRef(random);
  randomRef.current = random;
  const [held, setHeld] = useState<Held>(() => {
    const bucket = greetingBucket(new Date().getHours());
    return { bucket, line: pickGreeting(bucket, random) };
  });

  useEffect(() => {
    const refresh = () => {
      const bucket = greetingBucket(new Date().getHours());
      setHeld((prev) =>
        prev.bucket === bucket ? prev : { bucket, line: pickGreeting(bucket, randomRef.current) },
      );
    };
    const id = setInterval(refresh, TICK_MS);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, []);

  return held.line;
}
