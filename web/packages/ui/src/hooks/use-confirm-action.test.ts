import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useConfirmAction } from "@/hooks/use-confirm-action";

describe("useConfirmAction", () => {
  it("resolves true and leaves no error when the action succeeds", async () => {
    const { result } = renderHook(() => useConfirmAction());
    let done = false;
    await act(async () => {
      done = await result.current.run(async () => undefined);
    });
    expect(done).toBe(true);
    expect(result.current.error).toBeNull();
    expect(result.current.pending).toBe(false);
  });

  it("is pending while the action runs, then keeps the message verbatim when it throws", async () => {
    const { result } = renderHook(() => useConfirmAction());
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => (release = r));
    let outcome: Promise<boolean> = Promise.resolve(true);
    act(() => {
      outcome = result.current.run(async () => {
        await gate;
        throw new Error("not a git repository");
      });
    });
    expect(result.current.pending).toBe(true);
    await act(async () => {
      release();
      await outcome;
    });
    expect(await outcome).toBe(false);
    expect(result.current.pending).toBe(false);
    expect(result.current.error).toBe("not a git repository");
  });

  it("a new run clears the previous error; reset() clears it on demand", async () => {
    const { result } = renderHook(() => useConfirmAction());
    await act(async () => {
      await result.current.run(async () => {
        throw new Error("first");
      });
    });
    expect(result.current.error).toBe("first");
    await act(async () => {
      await result.current.run(async () => undefined);
    });
    expect(result.current.error).toBeNull();
    await act(async () => {
      await result.current.run(async () => {
        throw "plain string";
      });
    });
    expect(result.current.error).toBe("plain string");
    act(() => result.current.reset());
    expect(result.current.error).toBeNull();
  });
});
