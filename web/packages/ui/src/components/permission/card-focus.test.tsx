import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PermissionCard } from "@/components/permission/permission-card";
import { QuestionCard } from "@/components/permission/question-card";
import { useCardFocusStore } from "@/stores/card-focus-store";
import type { PendingPermission, PendingQuestion } from "@/hooks/use-run-timeline";

function pendingPermission(overrides: Partial<PendingPermission> = {}): PendingPermission {
  return {
    requestId: "req-1",
    summary: "Run a risky command",
    options: [
      { id: "allow-1", label: "Allow", kind: "allow_once" },
      { id: "deny-1", label: "Deny", kind: "reject_once" },
    ],
    ...overrides,
  };
}

function pendingQuestion(overrides: Partial<PendingQuestion> = {}): PendingQuestion {
  return {
    requestId: "q-req-1",
    request: {
      source: "agent",
      questions: [{ id: "q1", text: "Which environment?", kind: "single", options: [{ id: "prod", label: "Prod" }] }],
    },
    ...overrides,
  };
}

/** A text field focused before any effect runs, so the card's own mount focus never muddies the assertion. */
function Harness({ children }: { children: React.ReactNode }) {
  return (
    <>
      <textarea aria-label="composer" ref={(el) => el?.focus()} />
      {children}
    </>
  );
}

beforeEach(() => {
  useCardFocusStore.setState({ requests: {} });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("card focus: permission card", () => {
  it("takes focus on a matching request even while a text field has it, and consumes the request", () => {
    render(
      <Harness>
        <PermissionCard runId="run-1" pending={pendingPermission()} onRespond={vi.fn()} onChat={vi.fn()} />
      </Harness>,
    );
    const composer = screen.getByLabelText("composer");
    expect(document.activeElement).toBe(composer);

    act(() => {
      useCardFocusStore.getState().request("req-1");
    });

    expect(document.activeElement).toBe(screen.getByTestId("permission-card"));
    expect(useCardFocusStore.getState().requests["req-1"]).toBeUndefined();
  });

  it("ignores a request for another request id, leaving it queued", () => {
    render(
      <Harness>
        <PermissionCard runId="run-1" pending={pendingPermission()} onRespond={vi.fn()} onChat={vi.fn()} />
      </Harness>,
    );
    const composer = screen.getByLabelText("composer");

    act(() => {
      useCardFocusStore.getState().request("req-other");
    });

    expect(document.activeElement).toBe(composer);
    expect(useCardFocusStore.getState().requests["req-other"]).toBeDefined();
  });

  it("does not focus on a stale request", () => {
    vi.useFakeTimers();
    act(() => {
      useCardFocusStore.getState().request("req-1");
    });
    act(() => {
      vi.advanceTimersByTime(16_000);
    });

    render(
      <Harness>
        <PermissionCard runId="run-1" pending={pendingPermission()} onRespond={vi.fn()} onChat={vi.fn()} />
      </Harness>,
    );

    expect(document.activeElement).toBe(screen.getByLabelText("composer"));
    expect(useCardFocusStore.getState().requests["req-1"]).toBeUndefined();
  });
});

describe("card focus: question card", () => {
  it("takes focus on a matching request even while a text field has it, and consumes the request", () => {
    render(
      <Harness>
        <QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />
      </Harness>,
    );
    const composer = screen.getByLabelText("composer");
    expect(document.activeElement).toBe(composer);

    act(() => {
      useCardFocusStore.getState().request("q-req-1");
    });

    expect(document.activeElement).toBe(screen.getByTestId("question-card"));
    expect(useCardFocusStore.getState().requests["q-req-1"]).toBeUndefined();
  });

  it("ignores a request for another request id, leaving it queued", () => {
    render(
      <Harness>
        <QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />
      </Harness>,
    );
    const composer = screen.getByLabelText("composer");

    act(() => {
      useCardFocusStore.getState().request("req-other");
    });

    expect(document.activeElement).toBe(composer);
    expect(useCardFocusStore.getState().requests["req-other"]).toBeDefined();
  });

  it("does not focus on a stale request", () => {
    vi.useFakeTimers();
    act(() => {
      useCardFocusStore.getState().request("q-req-1");
    });
    act(() => {
      vi.advanceTimersByTime(16_000);
    });

    render(
      <Harness>
        <QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />
      </Harness>,
    );

    expect(document.activeElement).toBe(screen.getByLabelText("composer"));
    expect(useCardFocusStore.getState().requests["q-req-1"]).toBeUndefined();
  });
});
