import { afterEach, describe, expect, it, vi } from "vitest";

import { playNotificationSound } from "@/lib/notification-sound";

/** jsdom has no Web Audio API -- installs a minimal fake AudioContext so the happy path is exercised, not just the "unsupported" no-op branch. */
function installFakeAudioContext() {
  const oscillator = {
    type: "",
    frequency: { value: 0 },
    connect: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
    onended: null as (() => void) | null,
  };
  const gain = {
    gain: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() },
    connect: vi.fn(),
  };
  const close = vi.fn();
  class FakeAudioContext {
    currentTime = 0;
    createOscillator = () => oscillator;
    createGain = () => gain;
    close = close;
  }
  vi.stubGlobal("AudioContext", FakeAudioContext);
  return { oscillator, gain, close };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("playNotificationSound", () => {
  it("does nothing (and never throws) without an AudioContext", () => {
    vi.unstubAllGlobals();
    expect(() => playNotificationSound()).not.toThrow();
  });

  it("starts and stops an oscillator through a gain node when AudioContext is available", () => {
    const { oscillator, gain } = installFakeAudioContext();

    playNotificationSound();

    expect(oscillator.connect).toHaveBeenCalledWith(gain);
    expect(gain.connect).toHaveBeenCalled();
    expect(oscillator.start).toHaveBeenCalled();
    expect(oscillator.stop).toHaveBeenCalled();
  });

  it("closes the context once the oscillator ends", () => {
    const { oscillator, close } = installFakeAudioContext();

    playNotificationSound();
    oscillator.onended?.();

    expect(close).toHaveBeenCalled();
  });

  it("swallows a constructor that throws", () => {
    class ThrowingAudioContext {
      constructor() {
        throw new Error("blocked");
      }
    }
    vi.stubGlobal("AudioContext", ThrowingAudioContext);

    expect(() => playNotificationSound()).not.toThrow();
  });
});
