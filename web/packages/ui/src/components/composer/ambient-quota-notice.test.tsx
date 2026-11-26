import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AmbientQuotaNotice } from "@/components/composer/ambient-quota-notice";
import { Composer } from "@/components/composer/composer";
import { usageDataFrom } from "@/lib/account-usage/query";
import type { AccountUsage } from "@/lib/account-usage/types";
import { queryKeys } from "@/lib/query-keys";
import { __resetQuotaNoticeStoreForTests } from "@/lib/quota-notice-store";
import { claudeAccount, FIXTURE_NOW } from "@/test/account-usage-fixtures";
import { renderWithDaemon, type DaemonRender } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

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

/**
 * Puts the reports into the connection's shared usage cache the way a usage
 * surface or a completed fetch left them: the notice never loads them itself
 * (`useUsageReports({ enabled: false })`), so every test seeds the cache.
 */
async function seed(view: DaemonRender, usages: AccountUsage[]): Promise<void> {
  act(() => {
    view.queryClient().setQueryData(
      queryKeys.accountUsage(),
      usageDataFrom({ enabled: true, ttlSeconds: 300, reports: usages }, undefined),
    );
  });
  await flush();
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
    // jsdom's window is never focused; a real viewer's is (the app window showing the composer).
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() => "visible");
    vi.spyOn(document, "hasFocus").mockImplementation(() => true);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("ambient_quota_notice_shows_at_92_and_is_dismissible", async () => {
    const daemon = new FakeUsageDaemon([]);
    const view = mountNotice(daemon);
    await seed(view, [report({ fiveHourPct: 92 })]);

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
    const daemon = new FakeUsageDaemon([]);
    const first = mountNotice(daemon);
    await seed(first, [report({ fiveHourPct: 92 })]);
    fireEvent.click(screen.getByTestId("ambient-quota-dismiss"));
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    first.unmount();

    // A fresh page load (the store re-reads storage, the cache starts empty): still dismissed.
    __resetQuotaNoticeStoreForTests();
    const second = mountNotice(daemon);
    await seed(second, [report({ fiveHourPct: 92 })]);
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    second.unmount();

    // The memory is the stored key: without it the notice is back.
    window.localStorage.removeItem(DISMISSED_KEY);
    __resetQuotaNoticeStoreForTests();
    const third = mountNotice(daemon);
    await seed(third, [report({ fiveHourPct: 92 })]);
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });

  it("ambient_quota_new_resets_at_is_a_new_window", async () => {
    const resetsAt = FIXTURE_NOW + 2 * HOUR;
    const daemon = new FakeUsageDaemon([]);
    const view = mountNotice(daemon);
    await seed(view, [report({ resetsAtMs: resetsAt })]);
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
    const daemon = new FakeUsageDaemon([]);
    const view = mountNotice(daemon);
    await seed(view, [report({ fiveHourPct: 92, resetsAtMs: resetsAt })]);
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
    const at89 = new FakeUsageDaemon([]);
    const first = mountNotice(at89);
    await seed(first, [report({ fiveHourPct: 89 })]);
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    first.unmount();

    const at80 = new FakeUsageDaemon([]);
    const second = mountNotice(at80);
    await seed(second, [report({ fiveHourPct: 80 })]);
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
  });

  it("ambient_quota_no_notice_for_another_providers_account", async () => {
    const daemon = new FakeUsageDaemon([]);
    const claude = mountNotice(daemon, "claude-native");
    await seed(claude, [report({ fiveHourPct: 95, provider: "openai" })]);
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    claude.unmount();

    // The same account is the codex provider's own: it counts there.
    const codex = mountNotice(daemon, "codex-native");
    await seed(codex, [report({ fiveHourPct: 95, provider: "openai" })]);
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });

  it("ambient_quota_adds_no_touch_or_refresh_calls", async () => {
    const daemon = new FakeUsageDaemon([]);

    // A seeded cache: mounting, remounting and one live event send no request of any mode.
    const first = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon);
    await seed(first, [report({ fiveHourPct: 92 })]);
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
    first.unmount();

    // A remount on the same connection's QueryClient: the cached reports are already held.
    const queryClient = first.queryClient();
    const remount = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon, { queryClient });
    await flush();
    remount.unmount();

    // A completed fetch published while the notice is mounted: an event, never a request.
    const live = renderWithDaemon(<AmbientQuotaNotice provider="claude-native" />, daemon, { queryClient });
    await flush();
    await pushReport(daemon, report({ fiveHourPct: 93, fetchedAtMs: FIXTURE_NOW - 30_000 }));
    expect(screen.getByTestId("ambient-quota-text")).toHaveTextContent(/^5-hour window 93% used, resets \d\d:\d\d$/);
    live.unmount();

    expect(daemon.requests.length).toBe(0);
    expect(daemon.countCalls("account.usage")).toBe(0);
    expect(daemon.count("touch")).toBe(0);
    expect(daemon.count("refresh")).toBe(0);

    // An empty cache (nothing seeded, a cold start): no notice, and still zero requests.
    const empty = mountNotice(daemon);
    await flush();
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();
    empty.unmount();
    expect(daemon.requests.length).toBe(0);
  });

  it("ambient_quota_notice_is_in_flow_not_overlaying_the_composer", async () => {
    const daemon = new FakeUsageDaemon([]);
    const view = mountNotice(daemon);
    await seed(view, [report({ fiveHourPct: 92 })]);
    assertInFlow(screen.getByTestId("ambient-quota-notice"));
    view.unmount();

    const composer = mountComposer(new FakeUsageDaemon([]));
    await seed(composer, [report({ fiveHourPct: 92 })]);
    const form = screen.getByTestId("composer");
    const notice = screen.getByTestId("ambient-quota-notice");
    const card = screen.getByTestId("composer-card");
    expect(form).toContainElement(notice);
    expect(notice.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    composer.unmount();
  });

  it("ambient_quota_composer_mounts_the_notice", async () => {
    const at92 = mountComposer(new FakeUsageDaemon([]));
    await seed(at92, [report({ fiveHourPct: 92 })]);
    expect(at92.container.querySelector('[data-testid="ambient-quota-notice"]')).not.toBeNull();
    at92.unmount();

    const at50 = mountComposer(new FakeUsageDaemon([]));
    await seed(at50, [report({ fiveHourPct: 50 })]);
    expect(at50.container.querySelector('[data-testid="ambient-quota-notice"]')).toBeNull();
    at50.unmount();
  });
});
