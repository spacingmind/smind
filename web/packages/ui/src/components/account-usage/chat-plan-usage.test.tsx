import type { ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ChatUsageTotal } from "@/components/chat-usage-total";
import { RunUsagePill } from "@/components/run-usage-pill";
import { PlanUsageSourceProvider } from "@/hooks/use-chat-plan-usage";
import { closeUsageModal, useUsageModalOpen } from "@/hooks/use-usage-modal";
import { STORAGE_KEYS } from "@/lib/storage";
import type { RunUsage } from "@/lib/types";
import { claudeAccount, codexAccount, FIXTURE_NOW, problemAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function tokens(overrides: Partial<RunUsage> = {}): RunUsage {
  return { inputTokens: 1200, outputTokens: 300, source: "claude_result", ...overrides };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function ModalProbe() {
  return useUsageModalOpen() ? <div data-testid="usage-modal-open" /> : null;
}

function renderWith(daemon: FakeUsageDaemon, ui: ReactNode) {
  return render(
    <PlanUsageSourceProvider client={daemon.client} events={daemon.events}>
      {ui}
      <ModalProbe />
    </PlanUsageSourceProvider>,
  );
}

function chatPill(provider: string | null = "claude-native", runs = [{ usage: tokens() }]) {
  return <ChatUsageTotal runs={runs} provider={provider} />;
}

/** Opens the card the way a keyboard user does: focus (Radix opens a hover card on focus after its open delay). */
async function open(testId = "chat-usage-total") {
  await act(async () => {
    fireEvent.focus(screen.getByTestId(testId));
  });
  await screen.findByTestId(testId === "run-usage-pill" ? "run-usage-popover" : "chat-usage-total-popover", {}, { timeout: 3000 });
}

function setHoverNone(none: boolean): void {
  window.matchMedia = ((query: string) =>
    ({
      matches: none && query === "(hover: none)",
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
}

const originalMatchMedia = window.matchMedia;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(FIXTURE_NOW);
  localStorage.removeItem(STORAGE_KEYS.usage);
});

afterEach(() => {
  act(() => closeUsageModal());
  window.matchMedia = originalMatchMedia;
  localStorage.removeItem(STORAGE_KEYS.usage);
  vi.useRealTimers();
});

describe("chat usage hover card: Plan usage", () => {
  it("chat_pill_hover_shows_token_rows_and_plan_usage_for_the_chats_provider", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();

    const card = screen.getByTestId("chat-usage-total-popover");
    expect(card).toHaveAttribute("data-slot", "popover-content");
    expect(within(card).getByText("Usage total")).toBeInTheDocument();
    expect(within(card).getByText("1,200")).toBeInTheDocument();
    const section = within(card).getByTestId("chat-plan-usage");
    expect(within(section).getByText("Plan usage")).toBeInTheDocument();
    expect(within(section).getByText("Claude")).toBeInTheDocument();
    expect(within(section).getByText("max")).toBeInTheDocument();
    expect(within(section).getByTestId("plan-usage-window-five_hour")).toHaveTextContent("31% · resets 2h");
    expect(within(section).getByTestId("plan-usage-window-weekly")).toHaveTextContent("54% · resets 3d");
    expect(within(section).getByTestId("plan-usage-freshness")).toHaveTextContent("Updated 3m ago");
    // The other provider's account is not this chat's plan.
    expect(within(section).queryByTestId("plan-usage-2")).not.toBeInTheDocument();
  });

  it("pointer_can_move_into_the_card_without_closing_it", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    const pill = screen.getByTestId("chat-usage-total");
    await act(async () => {
      fireEvent.pointerEnter(pill, { pointerType: "mouse" });
    });
    const card = await screen.findByTestId("chat-usage-total-popover", {}, { timeout: 3000 });
    // the pointer leaves the pill for the card inside the close delay: still open
    await act(async () => {
      fireEvent.pointerLeave(pill, { pointerType: "mouse" });
      fireEvent.pointerEnter(card, { pointerType: "mouse" });
      await new Promise((r) => setTimeout(r, 400));
    });
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    // leaving the card closes it after the delay
    await act(async () => {
      fireEvent.pointerLeave(card, { pointerType: "mouse" });
    });
    await waitFor(() => expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument(), { timeout: 3000 });
  });

  it("focus_opens_and_escape_closes", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument();
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument();
  });

  it("run_pill_hover_uses_the_runs_provider", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderWith(daemon, <RunUsagePill usage={tokens({ source: "codex_token_usage" })} provider="codex-native" />);
    await flush();
    await open("run-usage-pill");
    const card = screen.getByTestId("run-usage-popover");
    expect(within(card).getByText("Source: Codex token usage", { exact: false })).toBeInTheDocument();
    const section = within(card).getByTestId("chat-plan-usage");
    expect(within(section).getByTestId("plan-usage-2")).toHaveTextContent("Codex");
    expect(within(section).getByTestId("plan-usage-window-five_hour")).toHaveTextContent("7%");
    expect(within(section).queryByTestId("plan-usage-1")).not.toBeInTheDocument();
  });

  it("a GLM chat shows no Plan usage section and sends no request", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill("glm"));
    await flush();
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-plan-usage")).not.toBeInTheDocument();
    expect(daemon.requests).toHaveLength(0);
  });

  it("one_oauth_account_shows_it_without_a_note", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getAllByTestId(/^plan-usage-\d+$/)).toHaveLength(1);
    expect(screen.queryByTestId("plan-usage-note")).not.toBeInTheDocument();
  });

  it("several_oauth_accounts_all_listed_compactly_filter_selected_first_with_the_untracked_login_note", async () => {
    localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ accountKey: "anthropic-3" }));
    const daemon = new FakeUsageDaemon([
      claudeAccount(),
      claudeAccount({ accountId: 3, key: "anthropic-3", displayName: "Work" }),
      codexAccount(),
    ]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    const rows = screen.getAllByTestId(/^plan-usage-\d+$/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual(["plan-usage-3", "plan-usage-1"]);
    expect(screen.getByTestId("plan-usage-note")).toHaveTextContent(
      "This chat's login isn't tracked; showing all Claude accounts",
    );
  });

  it("api_key_accounts_and_providers_without_oauth_accounts_show_no_section", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount({ credentialType: "api_key" }), codexAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-plan-usage")).not.toBeInTheDocument();
  });

  it("hover_sends_at_most_one_cached_list_and_never_touch_or_refresh", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    // 20 real open/close cycles: hover opens it, leaving closes it.
    for (let i = 0; i < 20; i++) {
      await act(async () => {
        fireEvent.pointerEnter(screen.getByTestId("chat-usage-total"), { pointerType: "mouse" });
      });
      await screen.findByTestId("chat-usage-total-popover", {}, { timeout: 3000 });
      await act(async () => {
        fireEvent.pointerLeave(screen.getByTestId("chat-usage-total"), { pointerType: "mouse" });
      });
      await waitFor(() => expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument(), { timeout: 3000 });
    }
    await act(async () => {
      fireEvent.click(screen.getByTestId("chat-usage-total"));
    });
    expect(screen.getByTestId("plan-usage-1")).toBeInTheDocument();
    expect(daemon.modes()).toEqual(["cached"]);
    expect(daemon.count("touch")).toBe(0);
    expect(daemon.count("refresh")).toBe(0);
  }, 30_000);

  it("a click opens the card with focus inside so Open Usage is reachable by keyboard, and a second click closes it", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await act(async () => {
      fireEvent.click(screen.getByTestId("chat-usage-total"));
    });
    const card = screen.getByTestId("chat-usage-total-popover");
    expect(card.contains(document.activeElement)).toBe(true);
    screen.getByTestId("plan-usage-open").focus();
    expect(document.activeElement).toBe(screen.getByTestId("plan-usage-open"));
    // pointer leaving does not dismiss a click-opened card
    await act(async () => {
      fireEvent.pointerLeave(screen.getByTestId("chat-usage-total"), { pointerType: "mouse" });
      await new Promise((r) => setTimeout(r, 400));
    });
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByTestId("chat-usage-total"));
    });
    expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument();
  });

  it("a hover-opened card does not steal focus", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, <><button data-testid="elsewhere">x</button>{chatPill()}</>);
    await flush();
    screen.getByTestId("elsewhere").focus();
    await act(async () => {
      fireEvent.pointerEnter(screen.getByTestId("chat-usage-total"), { pointerType: "mouse" });
    });
    await screen.findByTestId("chat-usage-total-popover", {}, { timeout: 3000 });
    expect(document.activeElement).toBe(screen.getByTestId("elsewhere"));
  });

  it("filter_on_another_providers_account_still_lists_this_chats_accounts", async () => {
    localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ accountKey: "openai-2" }));
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getAllByTestId(/^plan-usage-\d+$/).map((r) => r.getAttribute("data-testid"))).toEqual(["plan-usage-1"]);
  });

  it("a reshaped report (no windows, null report, non-string provider) renders without throwing", async () => {
    const odd = claudeAccount({ accountId: 5, key: "anthropic-5", displayName: "Odd" });
    odd.report = { status: "available", fetchedAt: "not a date", windows: null as never, planLabel: 7 as never };
    const nothing = claudeAccount({ accountId: 6, key: "anthropic-6", report: null, lastError: "timeout" });
    const daemon = new FakeUsageDaemon([claudeAccount(), odd, nothing]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("plan-usage-5")).toBeInTheDocument();
    expect(screen.getByTestId("plan-usage-6")).toHaveTextContent("Couldn't read usage: timeout");
  });

  it("an error list shows one muted line, a loading list a spinner, and neither sends more than one request", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    daemon.failWith = new Error("boom");
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("chat-plan-usage")).toHaveTextContent("Couldn't load plan usage");
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("a filled store is not listed again, by a chat pill and a run pill together", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, <>{chatPill()}<RunUsagePill usage={tokens()} provider="claude-native" /></>);
    await flush();
    await open("run-usage-pill");
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("empty_cache_says_no_plan_data_yet_and_open_usage_opens_the_modal", async () => {
    const daemon = new FakeUsageDaemon([]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("plan-usage-empty")).toHaveTextContent("No plan data yet");
    expect(screen.queryByTestId("usage-modal-open")).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByTestId("plan-usage-open"));
    });
    expect(screen.getByTestId("usage-modal-open")).toBeInTheDocument();
    // the card dismissed itself and nothing was fetched on its behalf
    expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument();
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("stale_data_shows_its_age_and_is_not_refetched", async () => {
    const stale = claudeAccount();
    stale.stale = true;
    stale.report = { ...stale.report!, fetchedAt: new Date(FIXTURE_NOW - 40 * 60_000).toISOString() };
    const daemon = new FakeUsageDaemon([stale]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("plan-usage-freshness")).toHaveTextContent("Updated 40m ago");
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("problem_state_shows_one_line_and_no_windows", async () => {
    const daemon = new FakeUsageDaemon([problemAccount("expired")]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("plan-usage-problem")).toHaveTextContent("Login expired 2h ago. Reconnect it in Providers.");
    expect(screen.queryByTestId("plan-usage-window-five_hour")).not.toBeInTheDocument();
  });

  it("card_has_no_pin_and_no_refresh_controls", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    const section = screen.getByTestId("chat-plan-usage");
    expect(within(section).getAllByRole("button").map((b) => b.textContent)).toEqual(["Open Usage"]);
    expect(within(section).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(within(section).queryByLabelText(/refresh|pin/i)).not.toBeInTheDocument();
  });

  it("honours_used_remaining", async () => {
    localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ displayAs: "remaining" }));
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("plan-usage-window-five_hour")).toHaveTextContent("69% left");
  });

  it("trigger_shown_when_only_plan_data_exists", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill("claude-native", []));
    await flush();
    const pill = screen.getByTestId("chat-usage-total");
    expect(pill).toHaveAccessibleName("Plan usage");
    await open();
    const card = screen.getByTestId("chat-usage-total-popover");
    expect(within(card).queryByText("Usage total")).not.toBeInTheDocument();
    expect(within(card).getByTestId("plan-usage-1")).toBeInTheDocument();
  });

  it("no trigger when there are neither tokens nor plan windows", async () => {
    const daemon = new FakeUsageDaemon([problemAccount("expired")]);
    renderWith(daemon, chatPill("claude-native", []));
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();
    const glm = renderWith(new FakeUsageDaemon([claudeAccount()]), chatPill("glm", []));
    await flush();
    expect(glm.queryByTestId("chat-usage-total")).not.toBeInTheDocument();
  });

  it("plan_section_absent_when_polling_disabled_or_daemon_lacks_account_usage", async () => {
    const off = new FakeUsageDaemon([claudeAccount()]);
    off.enabled = false;
    const first = renderWith(off, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-plan-usage")).not.toBeInTheDocument();
    first.unmount();

    const old = new FakeUsageDaemon([claudeAccount()]);
    old.makeOld();
    renderWith(old, chatPill());
    await flush();
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-plan-usage")).not.toBeInTheDocument();
  });

  it("without a shell source the card is the token card only", async () => {
    render(chatPill());
    await open();
    expect(screen.getByTestId("chat-usage-total-popover")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-plan-usage")).not.toBeInTheDocument();
  });

  it("touch_tap_opens_the_same_content_in_the_popover", async () => {
    setHoverNone(true);
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await act(async () => {
      fireEvent.pointerEnter(screen.getByTestId("chat-usage-total"), { pointerType: "touch" });
    });
    expect(screen.queryByTestId("chat-usage-total-popover")).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByTestId("chat-usage-total"));
    });
    const card = screen.getByTestId("chat-usage-total-popover");
    expect(card).toHaveAttribute("data-slot", "popover-content");
    expect(within(card).getByText("Usage total")).toBeInTheDocument();
    expect(within(card).getByTestId("plan-usage-window-five_hour")).toBeInTheDocument();
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("a live account.usage event updates an open card without a request", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWith(daemon, chatPill());
    await flush();
    await open();
    const fresh = claudeAccount();
    fresh.report = { ...fresh.report!, fetchedAt: new Date(FIXTURE_NOW + 60_000).toISOString(), windows: fresh.report!.windows!.map((w) => ({ ...w, usedPct: 88, remainingPct: 12 })) };
    await act(async () => {
      daemon.emit("account.usage", { usage: fresh });
    });
    expect(screen.getByTestId("plan-usage-window-five_hour")).toHaveTextContent("88%");
    expect(daemon.modes()).toEqual(["cached"]);
  });
});
