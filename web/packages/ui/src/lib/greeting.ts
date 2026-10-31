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

export function pickGreeting(bucket: GreetingBucket, random: () => number = Math.random): string {
  const lines = GREETINGS[bucket];
  return lines[Math.min(lines.length - 1, Math.floor(random() * lines.length))];
}
