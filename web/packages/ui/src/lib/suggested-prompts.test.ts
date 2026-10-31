import { describe, expect, it } from "vitest";
import { OFFICE_SUGGESTED_PROMPTS } from "@/lib/suggested-prompts";

describe("OFFICE_SUGGESTED_PROMPTS", () => {
  it("is exactly four distinct, non-empty prompts", () => {
    expect(OFFICE_SUGGESTED_PROMPTS).toHaveLength(4);
    expect(new Set(OFFICE_SUGGESTED_PROMPTS).size).toBe(4);
    for (const p of OFFICE_SUGGESTED_PROMPTS) expect(p.trim()).not.toBe("");
  });
});
