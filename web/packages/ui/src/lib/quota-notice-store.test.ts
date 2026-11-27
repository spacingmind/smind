import { beforeEach, describe, expect, it, vi } from "vitest";

import { quotaNoticeKey } from "@/lib/ambient-quota";
import {
  __resetQuotaNoticeStoreForTests,
  dismissQuotaNotice,
  readDismissedQuotaNotices,
  subscribeDismissedQuotaNotices,
} from "@/lib/quota-notice-store";
import { STORAGE_KEYS } from "@/lib/storage";

const NOW = Date.now();
const HOUR = 60 * 60 * 1000;

function key(accountId: number, resetsAtMs: number, level: "warn" | "full" = "warn"): string {
  return quotaNoticeKey(accountId, "five_hour", resetsAtMs, level);
}

beforeEach(() => {
  window.localStorage.clear();
  __resetQuotaNoticeStoreForTests();
});

describe("dismissed quota notices (ADR-0059 F)", () => {
  it("ambient_quota_storage_is_bounded_and_prunes_ended_windows", () => {
    // (a) a key dismissed twice is stored once and read back
    const once = key(1, NOW + HOUR);
    dismissQuotaNotice(once, NOW);
    dismissQuotaNotice(once, NOW);
    expect(readDismissedQuotaNotices()).toEqual([once]);

    // (b) 250 distinct keys leave the newest 200
    __resetQuotaNoticeStoreForTests();
    window.localStorage.clear();
    for (let accountId = 1; accountId <= 250; accountId++) dismissQuotaNotice(key(accountId, NOW + HOUR), NOW);
    const bounded = readDismissedQuotaNotices();
    expect(bounded).toHaveLength(200);
    expect(bounded[0]).toBe(key(51, NOW + HOUR));
    expect(bounded[199]).toBe(key(250, NOW + HOUR));
    expect(bounded).not.toContain(key(50, NOW + HOUR));

    // (c) a window that ended 25 hours ago is pruned by the next dismiss; 2 hours ago stays
    __resetQuotaNoticeStoreForTests();
    window.localStorage.clear();
    const endedLongAgo = key(1, NOW - 25 * HOUR);
    const endedRecently = key(2, NOW - 2 * HOUR);
    dismissQuotaNotice(endedLongAgo, NOW);
    dismissQuotaNotice(endedRecently, NOW);
    dismissQuotaNotice(key(3, NOW + HOUR), NOW);
    expect(readDismissedQuotaNotices()).toEqual([endedRecently, key(3, NOW + HOUR)]);

    // (d) corrupt stored JSON reads as []
    window.localStorage.setItem(STORAGE_KEYS.dismissedQuotaNotices, "{");
    __resetQuotaNoticeStoreForTests();
    expect(readDismissedQuotaNotices()).toEqual([]);

    // (e) a storage event from another tab refreshes the snapshot and notifies
    const listener = vi.fn();
    const unsubscribe = subscribeDismissedQuotaNotices(listener);
    const fromOtherTab = [key(9, NOW + HOUR)];
    window.localStorage.setItem(STORAGE_KEYS.dismissedQuotaNotices, JSON.stringify(fromOtherTab));
    window.dispatchEvent(new StorageEvent("storage"));
    expect(readDismissedQuotaNotices()).toEqual(fromOtherTab);
    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });
});
