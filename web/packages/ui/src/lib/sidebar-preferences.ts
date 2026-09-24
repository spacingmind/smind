/**
 * Client-side persistence for the sidebar's pinned tasks, unread tasks and
 * group-by view choice (web-sidebar-attention plan's Decisions: "All
 * client-side state" -- no daemon or wire change). Same read/validate/write
 * contract as every other localStorage-backed preference (see lib/storage.ts's
 * doc comment): reads never throw and fall back to empty/default on anything
 * unexpected, writes are best-effort.
 */

import { readStored, writeStored, STORAGE_KEYS } from "@/lib/storage";

function isNumberArray(value: unknown): value is number[] {
  return Array.isArray(value) && value.every((v) => typeof v === "number");
}

/** Reads the persisted set of pinned task ids, or an empty set if unset/corrupt. */
export function readPinnedTasks(): Set<number> {
  return new Set(readStored<number[]>(STORAGE_KEYS.pinnedTasks, [], isNumberArray));
}

export function writePinnedTasks(pinned: ReadonlySet<number>): void {
  writeStored(STORAGE_KEYS.pinnedTasks, [...pinned]);
}

/** Reads the persisted set of unread task ids, or an empty set if unset/corrupt. */
export function readUnreadTasks(): Set<number> {
  return new Set(readStored<number[]>(STORAGE_KEYS.unreadTasks, [], isNumberArray));
}

export function writeUnreadTasks(unread: ReadonlySet<number>): void {
  writeStored(STORAGE_KEYS.unreadTasks, [...unread]);
}

/** The sidebar's two task-list views: the default grouped workspace/space tree, or a flat group-by-status view (AC6). */
export type SidebarGroupMode = "tree" | "status";

function isSidebarGroupMode(value: unknown): value is SidebarGroupMode {
  return value === "tree" || value === "status";
}

export function readSidebarGroupMode(): SidebarGroupMode {
  return readStored<SidebarGroupMode>(STORAGE_KEYS.sidebarGroupMode, "tree", isSidebarGroupMode);
}

export function writeSidebarGroupMode(mode: SidebarGroupMode): void {
  writeStored(STORAGE_KEYS.sidebarGroupMode, mode);
}

/** Whether the Notifications settings section's sound toggle is on -- off by default (plan's Decisions). */
export function readNotificationSoundEnabled(): boolean {
  return readStored<boolean>(STORAGE_KEYS.notificationSound, false, (v): v is boolean => typeof v === "boolean");
}

export function writeNotificationSoundEnabled(enabled: boolean): void {
  writeStored(STORAGE_KEYS.notificationSound, enabled);
}
