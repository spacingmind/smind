import { afterEach, describe, expect, it, vi } from "vitest";
import { readInterfaceMode, writeInterfaceMode } from "@/lib/interface-mode";
import { STORAGE_KEYS } from "@/lib/storage";
import { useInterfaceModeStore } from "@/stores/interface-mode-store";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  useInterfaceModeStore.getState().reload();
});

describe("interface mode", () => {
  it("uses the documented key", () => {
    expect(STORAGE_KEYS.interfaceMode).toBe("smind:interface-mode");
  });

  it("reads_only_office", () => {
    expect(readInterfaceMode()).toBe("coding");
    for (const [raw, want] of [
      ["office", "office"],
      ["coding", "coding"],
      ["Office", "coding"],
      ['"office"', "coding"],
      ["true", "coding"],
      ["{", "coding"],
      ["", "coding"],
      ["anything-else", "coding"],
    ] as const) {
      window.localStorage.setItem(STORAGE_KEYS.interfaceMode, raw);
      expect(readInterfaceMode(), raw).toBe(want);
    }
  });

  it("corrupt_and_throwing_storage_fall_back", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(readInterfaceMode()).toBe("coding");
  });

  it("write_never_throws", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => writeInterfaceMode("office")).not.toThrow();
  });

  it("stores the bare string", () => {
    writeInterfaceMode("office");
    expect(window.localStorage.getItem(STORAGE_KEYS.interfaceMode)).toBe("office");
  });

  it("storage_event_from_other_tab", () => {
    expect(useInterfaceModeStore.getState().value).toBe("coding");
    window.localStorage.setItem(STORAGE_KEYS.interfaceMode, "office");
    window.dispatchEvent(new StorageEvent("storage", { key: STORAGE_KEYS.interfaceMode, newValue: "office" }));
    expect(useInterfaceModeStore.getState().value).toBe("office");
  });
});
