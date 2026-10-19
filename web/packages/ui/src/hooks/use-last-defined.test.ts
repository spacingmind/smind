import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useLastDefined } from "@/hooks/use-last-defined";

describe("useLastDefined", () => {
  it("keeps the last non-null value after the input is cleared (a dialog's target while it animates out)", () => {
    const { result, rerender } = renderHook(({ value }: { value: { name: string } | null }) => useLastDefined(value), {
      initialProps: { value: null as { name: string } | null },
    });
    expect(result.current).toBeNull();
    rerender({ value: { name: "playwright" } });
    expect(result.current).toEqual({ name: "playwright" });
    rerender({ value: null });
    expect(result.current).toEqual({ name: "playwright" });
    rerender({ value: { name: "other" } });
    expect(result.current).toEqual({ name: "other" });
  });
});
