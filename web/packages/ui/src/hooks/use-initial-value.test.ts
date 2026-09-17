import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useInitialValue } from "@/hooks/use-initial-value";

describe("useInitialValue", () => {
  it("returns the first value seen for a reset key", () => {
    const { result } = renderHook(({ value, resetKey }) => useInitialValue(value, resetKey), {
      initialProps: { value: 100, resetKey: "a" },
    });

    expect(result.current).toBe(100);
  });

  it("ignores later value changes while the reset key stays the same", () => {
    const { result, rerender } = renderHook(
      ({ value, resetKey }) => useInitialValue(value, resetKey),
      { initialProps: { value: 100, resetKey: "a" } },
    );

    rerender({ value: 250, resetKey: "a" });
    rerender({ value: 400, resetKey: "a" });

    expect(result.current).toBe(100);
  });

  it("re-freezes at the new value once the reset key changes", () => {
    const { result, rerender } = renderHook(
      ({ value, resetKey }) => useInitialValue(value, resetKey),
      { initialProps: { value: 100, resetKey: "a" } },
    );

    rerender({ value: 250, resetKey: "a" });
    rerender({ value: 420, resetKey: "b" });

    expect(result.current).toBe(420);

    rerender({ value: 999, resetKey: "b" });
    expect(result.current).toBe(420);
  });
});
