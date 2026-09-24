import { afterEach, describe, expect, it } from "vitest";

import {
  readNotificationSoundEnabled,
  readPinnedTasks,
  readSidebarGroupMode,
  readUnreadTasks,
  writeNotificationSoundEnabled,
  writePinnedTasks,
  writeSidebarGroupMode,
  writeUnreadTasks,
} from "@/lib/sidebar-preferences";

afterEach(() => {
  window.localStorage.clear();
});

describe("pinned tasks", () => {
  it("defaults to empty", () => {
    expect(readPinnedTasks()).toEqual(new Set());
  });

  it("round-trips across a write", () => {
    writePinnedTasks(new Set([1, 2, 3]));
    expect(readPinnedTasks()).toEqual(new Set([1, 2, 3]));
  });

  it("falls back to empty for corrupt storage", () => {
    window.localStorage.setItem("smind:pinned-tasks", "{not json");
    expect(readPinnedTasks()).toEqual(new Set());
  });
});

describe("unread tasks", () => {
  it("defaults to empty", () => {
    expect(readUnreadTasks()).toEqual(new Set());
  });

  it("round-trips across a write", () => {
    writeUnreadTasks(new Set([4, 5]));
    expect(readUnreadTasks()).toEqual(new Set([4, 5]));
  });
});

describe("sidebar group mode", () => {
  it("defaults to the tree view", () => {
    expect(readSidebarGroupMode()).toBe("tree");
  });

  it("round-trips across a write", () => {
    writeSidebarGroupMode("status");
    expect(readSidebarGroupMode()).toBe("status");
  });

  it("falls back to the tree view for an invalid stored value", () => {
    window.localStorage.setItem("smind:sidebar-group-mode", JSON.stringify("bogus"));
    expect(readSidebarGroupMode()).toBe("tree");
  });
});

describe("notification sound", () => {
  it("defaults to off", () => {
    expect(readNotificationSoundEnabled()).toBe(false);
  });

  it("round-trips across a write", () => {
    writeNotificationSoundEnabled(true);
    expect(readNotificationSoundEnabled()).toBe(true);
  });
});
