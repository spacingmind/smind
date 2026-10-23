import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { QueuedPrompts } from "@/components/composer/queued-prompts";
import { queueStartsLabel } from "@/lib/queue-hold";
import type { ChatQueueItem } from "@/lib/types";

function item(overrides: Partial<ChatQueueItem> = {}): ChatQueueItem {
  return {
    ID: 1, ChatID: 10, Prompt: "later", RunConfig: "{}", Source: "user", FromTaskID: null, FromChatID: null, Priority: 0,
    Status: "queued", RunID: null, CancelReason: "", CancelKind: "", CreatedAt: "", UpdatedAt: "", ...overrides,
  };
}

afterEach(() => vi.useRealTimers());

describe("queueStartsLabel", () => {
  const now = Date.parse("2026-10-22T10:00:00Z");
  it("is null without a hold, with garbage, or once the hold passed", () => {
    expect(queueStartsLabel(null, now)).toBeNull();
    expect(queueStartsLabel(undefined, now)).toBeNull();
    expect(queueStartsLabel("nope", now)).toBeNull();
    expect(queueStartsLabel("2026-10-22T09:59:59Z", now)).toBeNull();
  });
  it("counts seconds under a minute and shows local HH:MM beyond", () => {
    expect(queueStartsLabel("2026-10-22T10:00:18Z", now)).toBe("starts in 18s");
    const later = "2026-10-22T10:03:00Z";
    const hhmm = new Date(later).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
    expect(queueStartsLabel(later, now)).toBe(`starts ${hhmm}`);
  });
});

describe("QueuedPrompts child updates (ADR-0041)", () => {
  it("labels a child_event item 'Child updates' instead of its prompt, with its start time", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse("2026-10-22T10:00:00Z"));
    const prompt = '- task #12 "t" run r-1: done · cost not reported';
    render(<QueuedPrompts items={[item({ Source: "child_event", Prompt: prompt, NotBefore: "2026-10-22T10:03:00Z" })]} onCancel={() => {}} />);
    const row = screen.getByTestId("composer-queued-item");
    expect(row).toHaveTextContent("Child updates");
    expect(row).not.toHaveTextContent("task #12");
    expect(screen.getByTestId("queued-starts").textContent).toMatch(/^starts \d{2}:\d{2}$/);
  });

  it("counts down seconds and drops the label when the hold passes", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse("2026-10-22T10:00:00Z"));
    render(<QueuedPrompts items={[item({ Source: "child_event", NotBefore: "2026-10-22T10:00:18Z" })]} onCancel={() => {}} />);
    expect(screen.getByTestId("queued-starts")).toHaveTextContent("starts in 18s");
    act(() => void vi.advanceTimersByTime(5_000));
    expect(screen.getByTestId("queued-starts")).toHaveTextContent("starts in 13s");
    act(() => void vi.advanceTimersByTime(14_000));
    expect(screen.queryByTestId("queued-starts")).toBeNull();
  });

  it("renders items without NotBefore exactly as before", () => {
    render(<QueuedPrompts items={[item(), item({ ID: 2, NotBefore: null, Prompt: "next" })]} onCancel={() => {}} />);
    expect(screen.getAllByTestId("composer-queued-item").map((r) => r.textContent)).toEqual(["Queuedlater", "Queuednext"]);
    expect(screen.queryByTestId("queued-starts")).toBeNull();
  });
});
