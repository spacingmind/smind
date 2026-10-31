export type GreetingBucket = "late-night" | "morning" | "afternoon" | "evening";

/** Maps a local hour (0-23) to its part of the day. */
export function greetingBucket(hour: number): GreetingBucket {
  if (hour < 5) return "late-night";
  if (hour < 12) return "morning";
  if (hour < 18) return "afternoon";
  return "evening";
}

export const GREETINGS: Record<GreetingBucket, readonly string[]> = {
  "late-night": [
    "It's late — what's worth doing tonight",
    "Burning the midnight oil? Take care of yourself",
    "Quiet hours — keep it gentle and get some rest",
  ],
  morning: [
    "Good morning — what are we building today",
    "Fresh start — what's first on your list",
    "Morning — what would you like to work on",
  ],
  afternoon: [
    "Good afternoon — what shall we pick up next",
    "Afternoon — what's on your mind",
    "Welcome back — what are we working on",
  ],
  evening: [
    "Good evening — what's left to wrap up",
    "Evening — what would you like to tackle",
    "Winding down — what's worth finishing tonight",
  ],
};

/** Office mode's pool (ADR-0049 §1.6): the same four buckets, about documents and tasks. */
export const OFFICE_GREETINGS: Record<GreetingBucket, readonly string[]> = {
  "late-night": [
    "Working late — what needs writing tonight",
    "Quiet hours — what would you like to get done",
    "Still at it? Let's finish one thing and call it a night",
  ],
  morning: [
    "Good morning — what are we writing today",
    "Fresh start — which task comes first",
    "Morning — what should we get done today",
  ],
  afternoon: [
    "Good afternoon — which document shall we pick up",
    "Afternoon — what needs a closer look",
    "Welcome back — what are we finishing today",
  ],
  evening: [
    "Good evening — what is still open on your desk",
    "Evening — which task can we close out",
    "Winding down — which document needs a last pass",
  ],
};

export function pickGreeting(
  bucket: GreetingBucket,
  random: () => number = Math.random,
  pool: Record<GreetingBucket, readonly string[]> = GREETINGS,
): string {
  const lines = pool[bucket];
  return lines[Math.min(lines.length - 1, Math.floor(random() * lines.length))];
}
