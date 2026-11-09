import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";

import { RunErrorBox } from "@/components/timeline/run-error-box";
import type { RunEntry } from "@/hooks/use-run-timeline";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function entry(over: Partial<RunEntry>): RunEntry {
  return { id: "r1", provider: "glm", prompt: "p", status: "error", startedAt: "2026-11-01T09:59:00Z", items: [], err: "API Error: 401 authentication_error", ...over } as RunEntry;
}

describe("RunErrorBox failure classes", () => {
  it("auth_expired: the signed-out sentence and a Sign in button that opens Settings → Providers", () => {
    const seen: CustomEvent[] = [];
    const on = (e: Event) => seen.push(e as CustomEvent);
    window.addEventListener("smind:open-settings", on);
    render(<RunErrorBox run={entry({ failureKind: "auth_expired", failureAction: { kind: "sign_in", target: "settings:providers?account=7", label: "Sign in again" } })} />);
    expect(screen.getByTestId("run-error").textContent).toContain("signed out");
    fireEvent.click(screen.getByTestId("run-failure-signin"));
    window.removeEventListener("smind:open-settings", on);
    expect(seen).toHaveLength(1);
    expect(seen[0].detail).toEqual({ sectionId: "providers", accountId: 7 });
    expect(screen.queryByTestId("run-failure-retry")).toBeNull();
  });

  it("model_not_in_plan: names the allowed models, no control", () => {
    render(<RunErrorBox run={entry({ failureKind: "model_not_in_plan", failureAction: { kind: "pick_model", label: "Pick", models: ["sonnet", "haiku"] } })} />);
    expect(screen.getByTestId("run-error").textContent).toContain("not in your plan");
    expect(screen.getByTestId("run-failure-hint").textContent).toContain("sonnet, haiku");
    expect(screen.queryByTestId("run-failure-signin")).toBeNull();
    expect(screen.queryByTestId("run-failure-retry")).toBeNull();
  });

  it("transient_429 retrying: a live countdown that ticks down; no Retry button", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-01T10:00:00Z"));
    render(<RunErrorBox run={entry({ failureKind: "transient_429", failureAction: { kind: "auto_retry", label: "x", attempt: 1, max: 2, nextAt: "2026-11-01T10:00:30Z" } })} />);
    expect(screen.getByTestId("run-failure-countdown").textContent).toBe("Retrying in 30s (1 of 2)");
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByTestId("run-failure-countdown").textContent).toBe("Retrying in 20s (1 of 2)");
    expect(screen.queryByTestId("run-failure-retry")).toBeNull();
  });

  it("transient_429 spent: no countdown and no second Retry (the footer owns it)", () => {
    const run = entry({ failureKind: "transient_429", failureAction: { kind: "retry_now", label: "Retry" } });
    render(<RunErrorBox run={run} />);
    expect(screen.queryByTestId("run-failure-countdown")).toBeNull();
    expect(screen.queryByTestId("run-failure-retry")).toBeNull();
    expect(screen.getByTestId("run-failure-hint").textContent).toContain("short throttle");
  });

  it("plan_window_exhausted keeps the old error box (no class control)", () => {
    render(<RunErrorBox run={entry({ failureKind: "plan_window_exhausted", failureWindow: "5h", err: "429 Usage limit reached for 5 hour" })} />);
    expect(screen.getByTestId("run-error")).toBeTruthy();
    expect(screen.queryByTestId("run-failure-signin")).toBeNull();
    expect(screen.queryByTestId("run-failure-retry")).toBeNull();
  });

  it("context_window_exceeded: the daemon's summary, the static reason as hint, and Start a fresh chat dispatches smind:new-chat", () => {
    const seen: CustomEvent[] = [];
    const on = (e: Event) => seen.push(e as CustomEvent);
    window.addEventListener("smind:new-chat", on);
    render(
      <RunErrorBox
        run={entry({
          failureKind: "context_window_exceeded",
          failureAction: { kind: "new_chat", target: "chat:new", label: "Start a fresh chat for this task", summary: "The conversation no longer fits the model's context window." },
        })}
      />,
    );
    expect(screen.getByTestId("run-error").textContent).toBe("The conversation no longer fits the model's context window.");
    expect(screen.getByTestId("run-failure-hint").textContent).toBe("Earlier turns stay in this chat.");
    fireEvent.click(screen.getByTestId("run-failure-new-chat"));
    window.removeEventListener("smind:new-chat", on);
    expect(seen).toHaveLength(1);
    expect(screen.queryByTestId("run-failure-signin")).toBeNull();
  });

  it("session_budget_exceeded: the fallback sentence without a summary, the label as hint, no control", () => {
    render(<RunErrorBox run={entry({ failureKind: "session_budget_exceeded", failureAction: { kind: "raise_budget", label: "Raise the session budget in the provider's settings, or start a fresh chat" } })} />);
    expect(screen.getByTestId("run-error").textContent).toBe("The provider's session budget for this run is used up.");
    expect(screen.getByTestId("run-failure-hint").textContent).toContain("Raise the session budget");
    expect(screen.queryByTestId("run-failure-signin")).toBeNull();
    expect(screen.queryByTestId("run-failure-new-chat")).toBeNull();
    expect(screen.queryByTestId("run-failure-review-policy")).toBeNull();
  });

  it("too_many_denials: Review the permission policy opens Settings through the deep-link event", () => {
    const seen: CustomEvent[] = [];
    const on = (e: Event) => seen.push(e as CustomEvent);
    window.addEventListener("smind:open-settings", on);
    render(
      <RunErrorBox
        run={entry({
          failureKind: "too_many_denials",
          failureAction: { kind: "review_policy", target: "settings:permission-policy", label: "Review the permission policy", summary: "The agent kept asking for actions that were denied, so the provider stopped the run." },
        })}
      />,
    );
    expect(screen.getByTestId("run-error").textContent).toBe("The agent kept asking for actions that were denied, so the provider stopped the run.");
    // The control's own words are not repeated as a hint.
    expect(screen.queryByTestId("run-failure-hint")).toBeNull();
    fireEvent.click(screen.getByTestId("run-failure-review-policy"));
    window.removeEventListener("smind:open-settings", on);
    expect(seen).toHaveLength(1);
    expect(seen[0].detail).toEqual({ sectionId: "permission-policy" });
  });

  it("cli_exited: the readable sentence, a warning card, no control", () => {
    render(<RunErrorBox run={entry({ failureKind: "cli_exited", failureAction: { kind: "continue", label: "Continue the run" } })} />);
    expect(screen.getByTestId("run-error").textContent).toBe("The agent stopped before finishing; its session is saved, so you can continue where it left off.");
    expect(screen.queryByTestId("run-failure-hint")).toBeNull();
    expect(screen.queryByTestId("run-failure-signin")).toBeNull();
    expect(screen.queryByTestId("run-failure-new-chat")).toBeNull();
    expect(screen.queryByTestId("run-failure-review-policy")).toBeNull();
  });
});
