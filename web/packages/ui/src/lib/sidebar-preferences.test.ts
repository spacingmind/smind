import { afterEach, describe, expect, it } from "vitest";

import {
  readCollapsedNodes,
  readNotificationSoundEnabled,
  readPinnedTasks,
  readSidebarGroupMode,
  readUnreadTasks,
  writeCollapsedNodes,
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
    writeSidebarGroupMode("stage");
    expect(readSidebarGroupMode()).toBe("stage");
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

describe("collapsed sidebar nodes", () => {
  it("defaults to nothing collapsed", () => {
    expect(readCollapsedNodes()).toEqual(new Set());
  });

  it("round-trips string ids", () => {
    writeCollapsedNodes(new Set(["ws:1", "space:10", "pinned", "status:running"]));
    expect(readCollapsedNodes()).toEqual(new Set(["ws:1", "space:10", "pinned", "status:running"]));
  });

  it("removes the key when nothing is collapsed", () => {
    writeCollapsedNodes(new Set(["ws:1"]));
    writeCollapsedNodes(new Set());
    expect(window.localStorage.getItem("smind:sidebar-collapsed-nodes")).toBeNull();
  });

  it("falls back to nothing collapsed for corrupt or wrongly shaped storage", () => {
    window.localStorage.setItem("smind:sidebar-collapsed-nodes", "{not json");
    expect(readCollapsedNodes()).toEqual(new Set());
    window.localStorage.setItem("smind:sidebar-collapsed-nodes", JSON.stringify([1, 2]));
    expect(readCollapsedNodes()).toEqual(new Set());
    window.localStorage.setItem("smind:sidebar-collapsed-nodes", JSON.stringify({ ws: 1 }));
    expect(readCollapsedNodes()).toEqual(new Set());
  });
});
