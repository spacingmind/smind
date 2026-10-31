import { afterEach, describe, expect, it, vi } from "vitest";

import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { usePendingSendStore } from "@/stores/pending-send-store";

const config: RunConfigState = {
  baseAgentId: null,
  custom: false,
  provider: "claude-native",
  permissionMode: "",
  autoAccept: false,
  thinkingLevel: "",
  model: "",
};

describe("pending send store", () => {
  afterEach(() => {
    vi.useRealTimers();
    usePendingSendStore.setState({ entry: null });
  });

  it("take is atomic", () => {
    usePendingSendStore.getState().set(1, config);
    expect(usePendingSendStore.getState().take(1)).toEqual(config);
    expect(usePendingSendStore.getState().take(1)).toBeNull();
  });

  it("expires after the ttl", () => {
    vi.useFakeTimers();
    usePendingSendStore.getState().set(1, config);
    vi.advanceTimersByTime(30_001);
    expect(usePendingSendStore.getState().peek(1)).toBe(false);
    expect(usePendingSendStore.getState().take(1)).toBeNull();
    expect(usePendingSendStore.getState().entry).toBeNull();
  });

  it("other task does not take", () => {
    usePendingSendStore.getState().set(1, config);
    expect(usePendingSendStore.getState().take(2)).toBeNull();
    expect(usePendingSendStore.getState().peek(1)).toBe(true);
    expect(usePendingSendStore.getState().take(1)).toEqual(config);
  });
});
