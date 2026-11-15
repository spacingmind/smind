import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

// The three reading-column caps (the log, the permission dock, the composer)
// share one rule: 768px, widening to 896px only once the chat container is
// 1100px wide, so source text is the thing to assert on.
const WIDENED = "max-w-3xl @[1100px]/chat:max-w-4xl";

const taskDetail = readFileSync(resolve(__dirname, "task-detail.tsx"), "utf-8");
const composer = readFileSync(resolve(__dirname, "composer/composer.tsx"), "utf-8");

const occurrences = (source: string, needle: string): number => source.split(needle).length - 1;

describe("chat column width", () => {
  it("column-widens-only-from-1100px", () => {
    expect(taskDetail).toContain("@container/chat");
    expect(occurrences(taskDetail, WIDENED)).toBe(2);
    expect(occurrences(composer, WIDENED)).toBe(1);
  });
});
