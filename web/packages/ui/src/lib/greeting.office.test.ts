import { describe, expect, it } from "vitest";
import { GREETINGS, OFFICE_GREETINGS, pickGreeting, type GreetingBucket } from "@/lib/greeting";

const BUCKETS: GreetingBucket[] = ["late-night", "morning", "afternoon", "evening"];

describe("OFFICE_GREETINGS", () => {
  it("office_pool_per_bucket", () => {
    for (const bucket of BUCKETS) {
      expect(OFFICE_GREETINGS[bucket].length).toBeGreaterThanOrEqual(3);
      for (const line of OFFICE_GREETINGS[bucket]) expect(GREETINGS[bucket]).not.toContain(line);
      expect(new Set(OFFICE_GREETINGS[bucket]).size).toBe(OFFICE_GREETINGS[bucket].length);
    }
  });

  it("picks from the given pool and defaults to the coding one", () => {
    expect(OFFICE_GREETINGS.morning).toContain(pickGreeting("morning", () => 0, OFFICE_GREETINGS));
    expect(GREETINGS.morning).toContain(pickGreeting("morning", () => 0));
    expect(pickGreeting("evening", () => 0.999999, OFFICE_GREETINGS)).toBe(OFFICE_GREETINGS.evening.at(-1));
  });
});
