import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { trackWindowActive } from "@/lib/window-active";

let hasFocus = true;
beforeEach(() => {
  vi.useFakeTimers();
  hasFocus = true;
  vi.spyOn(document, "hasFocus").mockImplementation(() => hasFocus);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  delete document.documentElement.dataset.windowActive;
});

describe("trackWindowActive", () => {
  it("starts from the document's focus and follows window blur and focus", () => {
    const stop = trackWindowActive();
    expect(document.documentElement.dataset.windowActive).toBe("true");
    hasFocus = false;
    window.dispatchEvent(new Event("blur"));
    vi.runAllTimers();
    expect(document.documentElement.dataset.windowActive).toBe("false");
    hasFocus = true;
    window.dispatchEvent(new Event("focus"));
    vi.runAllTimers();
    expect(document.documentElement.dataset.windowActive).toBe("true");
    stop();
  });

  it("a blur that only moves focus inside the page (an iframe) keeps the window active", () => {
    const stop = trackWindowActive();
    window.dispatchEvent(new Event("blur")); // hasFocus is still true
    vi.runAllTimers();
    expect(document.documentElement.dataset.windowActive).toBe("true");
    stop();
  });

  it("reads the focus after the event, not during it: a blur that is followed by focus inside the page stays active", () => {
    const stop = trackWindowActive();
    hasFocus = false; // the instant of the blur event: focus is between elements
    window.dispatchEvent(new Event("blur"));
    hasFocus = true; // focus has landed in an iframe by the next tick
    vi.runAllTimers();
    expect(document.documentElement.dataset.windowActive).toBe("true");
    stop();
  });

  it("starts inactive when the window is not focused, and the cleanup removes the attribute and the listeners", () => {
    hasFocus = false;
    const stop = trackWindowActive();
    expect(document.documentElement.dataset.windowActive).toBe("false");
    stop();
    expect(document.documentElement.dataset.windowActive).toBeUndefined();
    hasFocus = true;
    window.dispatchEvent(new Event("focus"));
    vi.runAllTimers();
    expect(document.documentElement.dataset.windowActive).toBeUndefined();
  });
});
