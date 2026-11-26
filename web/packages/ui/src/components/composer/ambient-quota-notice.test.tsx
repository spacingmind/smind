import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AmbientQuotaNotice } from "@/components/composer/ambient-quota-notice";
import { Composer } from "@/components/composer/composer";
import { __resetQuotaNoticeStoreForTests } from "@/lib/quota-notice-store";
import { claudeAccount, FIXTURE_NOW } from "@/test/account-usage-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import type { AccountUsage } from "@/lib/account-usage/types";

const HOUR = 3_600_000;
const DISMISSED_KEY = "smind:dismissed-quota-notices";

/** Flushes pending microtasks inside `act` so React commits before assertions. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/**
 * The composer provider's account reading `fiveHourPct` used in a 5-hour
 * window resetting at `resetsAtMs` (default: two hours from FIXTURE_NOW),
 * fetched a minute ago. A fresher `fetchedAtMs` makes a live `account.usage`
 * event payload win over the cached copy.
 */
function report(over: { fiveHourPct?: number; resetsAtMs?: number; provider?: string; fetchedAtMs?: number } = {}): AccountUsage {
  const usedPct = over.fiveHourPct ?? 92;
  return claudeAccount({
    provider: over.provider ?? "anthropic",
    stale: false,
    report: {
      v: 2,
      status: "available",
      planLabel: "max",
      windows: [
        {
          id: "five_hour",
          label: "5-hour",
          usedPct,
          remainingPct: 100 - usedPct,
          resetsAt: new Date(over.resetsAtMs ?? FIXTURE_NOW + 2 * HOUR).toISOString(),
        },
      ],
      fetchedAt: new Date(over.fetchedAtMs ?? FIXTURE_NOW - 60_000).toISOString(),
    },
  });
}

/** Serves `usage` as a live daemon event, the way a fetched report arrives. */
async function pushReport(daemon: FakeUsageDaemon, usage: AccountUsage): Promise<void> {
  daemon.reports = [usage];
  await act(async () => daemon.emit("account.usage", { usage }));
  await flush();
}

function mountNotice(daemon: FakeUsageDaemon, provider = "claude-native") {
  return renderWithDaemon(<AmbientQuotaNotice provider={provider} />, daemon);
}

/** The real Composer against the fake daemon (the run config's default provider is claude-native). */
function mountComposer(daemon: FakeUsageDaemon) {
  return renderWithDaemon(
    <Composer
      client={daemon.client}
      taskId={1}
      chatId={10}
      isDefaultChat
      boundProvider={null}
      connected
      runningRunId={null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async () => ({ kind: "started", runId: "run-test" })}
      onStop={async () => {}}
    />,
    daemon,
  );
}

/** The element and every ancestor up to the body: none may be positioned out of the flow. */
function assertInFlow(el: HTMLElement): void {
  for (let node: HTMLElement | null = el; node && node !== document.body; node = node.parentElement) {
    expect(node.getAttribute("class") ?? "").not.toMatch(/\b(absolute|fixed|sticky)\b/);
  }
}

describe("ambient quota notice", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetQuotaNoticeStoreForTests();
    // Only Date is faked: the shared flush is microtask-only, so faking timers would hang rendering.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(FIXTURE_NOW);
  });

  afterEach(() => vi.useRealTimers());

  it("ambient_quota_notice_shows_at_92_and_is_dismissible", async () => {
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 92 })]);
    mountNotice(daemon);
    await flush();

    const banner = screen.getByTestId("ambient-quota-notice");
    expect(screen.getByTestId("ambient-quota-text")).toHaveTextContent(/^5-hour window 92% used, resets \d\d:\d\d$/);
    // The banner announces itself as a live region with a labelled dismiss control.
    expect(screen.getByRole("status")).toBe(banner);
    const dismiss = screen.getByRole("button", { name: "Dismiss the quota notice" });
    expect(dismiss).toBe(screen.getByTestId("ambient-quota-dismiss"));

    fireEvent.click(dismiss);
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    expect(JSON.parse(window.localStorage.getItem(DISMISSED_KEY) ?? "[]")).toHaveLength(1);
  });

  it("ambient_quota_dismissal_persists_across_remounts", async () => {
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 92 })]);
    const first = mountNotice(daemon);
    await flush();
    fireEvent.click(screen.getByTestId("ambient-quota-dismiss"));
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    first.unmount();

    // A fresh page load (the store re-reads storage): still dismissed.
    __resetQuotaNoticeStoreForTests();
    const second = mountNotice(daemon);
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    second.unmount();

    // The memory is the stored key: without it the notice is back.
    window.localStorage.removeItem(DISMISSED_KEY);
    __resetQuotaNoticeStoreForTests();
    mountNotice(daemon);
    await flush();
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });

  it("ambient_quota_new_resets_at_is_a_new_window", async () => {
    const resetsAt = FIXTURE_NOW + 2 * HOUR;
    const daemon = new FakeUsageDaemon([report({ resetsAtMs: resetsAt })]);
    mountNotice(daemon);
    await flush();
    fireEvent.click(screen.getByTestId("ambient-quota-dismiss"));
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();

    // Seconds of vendor jitter land in the same 5-minute bucket: the same dismissed window.
    await pushReport(daemon, report({ resetsAtMs: resetsAt + 30_000, fetchedAtMs: FIXTURE_NOW - 50_000 }));
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();

    // A window that resets hours later is a new one: the notice returns.
    await pushReport(daemon, report({ resetsAtMs: resetsAt + 5 * HOUR, fetchedAtMs: FIXTURE_NOW - 40_000 }));
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });

  it("ambient_quota_full_after_dismissed_warn_shows_once", async () => {
    const resetsAt = FIXTURE_NOW + 2 * HOUR;
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 92, resetsAtMs: resetsAt })]);
    mountNotice(daemon);
    await flush();
    fireEvent.click(screen.getByTestId("ambient-quota-dismiss"));
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();

    await pushReport(daemon, report({ fiveHourPct: 100, resetsAtMs: resetsAt, fetchedAtMs: FIXTURE_NOW - 50_000 }));
    const banner = screen.getByTestId("ambient-quota-notice");
    expect(screen.getByTestId("ambient-quota-text")).toHaveTextContent(/^5-hour window used up, resets \d\d:\d\d$/);
    expect(banner).toHaveAttribute("data-tone", "error");
    expect(banner.className).toContain("destructive");

    fireEvent.click(screen.getByTestId("ambient-quota-dismiss"));
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
  });

  it("ambient_quota_no_notice_below_threshold", async () => {
    const at89 = new FakeUsageDaemon([report({ fiveHourPct: 89 })]);
    const first = mountNotice(at89);
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    first.unmount();

    const at80 = new FakeUsageDaemon([report({ fiveHourPct: 80 })]);
    mountNotice(at80);
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
  });

  it("ambient_quota_no_notice_for_another_providers_account", async () => {
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 95, provider: "openai" })]);
    const claude = mountNotice(daemon, "claude-native");
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    claude.unmount();

    // The same account is the codex provider's own: it counts there.
    mountNotice(daemon, "codex-native");
    await flush();
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });

  it("ambient_quota_adds_no_touch_or_refresh_calls", async () => {
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 92 })]);
    const first = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon);
    await flush();
    first.unmount();

    // A remount on the same connection's QueryClient: the cached reports are already held.
    const queryClient = first.queryClient();
    const remount = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon, { queryClient });
    await flush();
    remount.unmount();

    expect(daemon.count("touch")).toBe(0);
    expect(daemon.count("refresh")).toBe(0);
    expect(daemon.requests.every((r) => r.mode === "cached")).toBe(true);
    expect(daemon.requests.length).toBeLessThanOrEqual(1);

    // Mounting again with the cache filled sends no new account.usage request.
    const cachedReads = daemon.requests.length;
    const again = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon, { queryClient });
    await flush();
    again.unmount();
    expect(daemon.requests).toHaveLength(cachedReads);
  });

  it("ambient_quota_notice_is_in_flow_not_overlaying_the_composer", async () => {
    const daemon = new FakeUsageDaemon([report({ fiveHourPct: 92 })]);
    const view = mountNotice(daemon);
    await flush();
    assertInFlow(screen.getByTestId("ambient-quota-notice"));
    view.unmount();

    const composer = mountComposer(new FakeUsageDaemon([report({ fiveHourPct: 92 })]));
    await flush();
    const form = screen.getByTestId("composer");
    const notice = screen.getByTestId("ambient-quota-notice");
    const card = screen.getByTestId("composer-card");
    expect(form).toContainElement(notice);
    expect(notice.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    composer.unmount();
  });

  it("ambient_quota_composer_mounts_the_notice", async () => {
    const at92 = mountComposer(new FakeUsageDaemon([report({ fiveHourPct: 92 })]));
    await flush();
    expect(at92.container.querySelector('[data-testid="ambient-quota-notice"]')).not.toBeNull();
    at92.unmount();

    const at50 = mountComposer(new FakeUsageDaemon([report({ fiveHourPct: 50 })]));
    await flush();
    expect(at50.container.querySelector('[data-testid="ambient-quota-notice"]')).toBeNull();
    at50.unmount();
  });
});
