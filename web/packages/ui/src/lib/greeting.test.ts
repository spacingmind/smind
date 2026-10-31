import { describe, expect, it } from "vitest";
import { GREETINGS, greetingBucket, type GreetingBucket } from "./greeting";

const at = (h: number, m: number) => greetingBucket(new Date(2026, 9, 31, h, m).getHours());

describe("greeting", () => {
  it("bucket edges 04:59/05:00", () => {
    expect(at(4, 59)).toBe("late-night");
    expect(at(5, 0)).toBe("morning");
  });
  it("bucket edges 11:59/12:00", () => {
    expect(at(11, 59)).toBe("morning");
    expect(at(12, 0)).toBe("afternoon");
  });
  it("bucket edges 17:59/18:00", () => {
    expect(at(17, 59)).toBe("afternoon");
    expect(at(18, 0)).toBe("evening");
  });
  it("bucket edges 23:59/00:00", () => {
    expect(at(23, 59)).toBe("evening");
    expect(at(0, 0)).toBe("late-night");
  });
  it("every bucket has 3 lines", () => {
    for (const b of Object.keys(GREETINGS) as GreetingBucket[]) {
      expect(GREETINGS[b]).toHaveLength(3);
    }
  });
});
