import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { QuestionCard } from "@/components/permission/question-card";
import type { PendingQuestion } from "@/hooks/use-run-timeline";
import type { QuestionAnswer } from "@/lib/types";

function pendingQuestion(overrides: Partial<PendingQuestion> = {}): PendingQuestion {
  return {
    requestId: "q-req-1",
    request: {
      source: "agent",
      questions: [
        {
          id: "q1",
          header: "Deploy",
          text: "Which environment?",
          kind: "single",
          options: [
            { id: "prod", label: "Prod", description: "The production cluster" },
            { id: "staging", label: "Staging" },
          ],
        },
        { id: "q2", text: "Pick services", kind: "multi", options: [{ id: "api", label: "API" }, { id: "web", label: "Web" }], allowOther: true },
        { id: "q3", text: "Anything else?", kind: "text" },
      ],
    },
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function answerAll(card: HTMLElement) {
  fireEvent.click(within(card).getByTestId("question-option-q1-prod"));
  fireEvent.click(within(card).getByTestId("question-option-q2-api"));
  fireEvent.change(within(card).getByTestId("question-text-q3"), { target: { value: "thanks" } });
}

describe("QuestionCard: rendering", () => {
  it("renders single as radios, multi as checkboxes, text as a textarea, with descriptions under their options", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);

    const card = screen.getByTestId("question-card");
    expect(within(card).getByTestId("question-option-q1-prod")).toHaveAttribute("type", "radio");
    expect(within(card).getByTestId("question-option-q2-api")).toHaveAttribute("type", "checkbox");
    expect(within(card).getByTestId("question-text-q3").tagName).toBe("TEXTAREA");
    expect(within(card).getByText("The production cluster")).toBeInTheDocument();
    expect(within(card).getByText("Deploy")).toBeInTheDocument();
    expect(within(card).getByText("Which environment?")).toBeInTheDocument();
  });

  it("preselects nothing", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);

    const card = screen.getByTestId("question-card");
    for (const input of card.querySelectorAll("input[type=radio], input[type=checkbox]")) {
      expect((input as HTMLInputElement).checked).toBe(false);
    }
    expect(screen.getByTestId("question-text-q3")).toHaveValue("");
    expect(screen.queryByTestId("question-other-q2")).toHaveValue("");
  });

  it("shows the shortened notice only when the daemon truncated the request", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    expect(screen.queryByText("Some of this text was shortened.")).not.toBeInTheDocument();

    render(
      <QuestionCard runId="run-2" pending={pendingQuestion({ request: { source: "agent", questions: [], truncated: true } })} onAnswer={vi.fn()} />,
    );
    expect(screen.getByText("Some of this text was shortened.")).toBeInTheDocument();
  });

  it("renders html, markdown and links in agent text as literal plain text with no anchor", () => {
    const hostile = 'Run <a href="https://evil.example">this</a> or [click](https://evil.example) `<script>alert(1)</script>`';
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({
          request: {
            source: "agent",
            questions: [
              { id: "q1", text: hostile, kind: "text" },
              { id: "q2", text: "Pick", kind: "single", options: [{ id: "a", label: hostile, description: hostile }] },
            ],
          },
        })}
        onAnswer={vi.fn()}
      />,
    );

    const card = screen.getByTestId("question-card");
    expect(card.textContent).toContain("<a href=\"https://evil.example\">this</a>");
    expect(card.textContent).toContain("[click](https://evil.example)");
    expect(card.querySelectorAll("a")).toHaveLength(0);
    expect(card.querySelectorAll("script")).toHaveLength(0);
  });

  it("renders long and secret-looking text unchanged", () => {
    const long = `sk-ant-${"x".repeat(400)}\n${"line\n".repeat(60)}`;
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({ request: { source: "agent", questions: [{ id: "q1", text: long, kind: "text" }] } })}
        onAnswer={vi.fn()}
      />,
    );
    const card = screen.getByTestId("question-card");
    expect(card.textContent).toContain(`sk-ant-${"x".repeat(400)}`);
    expect(card.textContent).toContain("line\n".repeat(60).trimEnd());
  });

  it("is focusable as a group and moves focus to itself on mount, unless a text field has focus", () => {
    const { rerender } = render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    const group = screen.getByRole("group", { name: "Agent question: Which environment?" });
    expect(group).toHaveAttribute("tabindex", "-1");
    expect(document.activeElement).toBe(group);

    // A new request re-focuses the card, but never steals focus from a field mid-keystroke.
    rerender(
      <>
        <textarea aria-label="composer" />
        <QuestionCard runId="run-1" pending={pendingQuestion({ requestId: "q-req-2" })} onAnswer={vi.fn()} />
      </>,
    );
    const composer = screen.getByLabelText("composer");
    composer.focus();
    rerender(
      <>
        <textarea aria-label="composer" />
        <QuestionCard runId="run-1" pending={pendingQuestion({ requestId: "q-req-3" })} onAnswer={vi.fn()} />
      </>,
    );
    expect(document.activeElement).toBe(composer);
  });
});

describe("QuestionCard: submit and decline payloads", () => {
  it("keeps Submit disabled until every question has a valid answer", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    const submit = screen.getByTestId("question-submit");
    expect(submit).toBeDisabled();

    fireEvent.click(screen.getByTestId("question-option-q1-prod"));
    expect(submit).toBeDisabled(); // q2, q3 still unanswered

    fireEvent.click(screen.getByTestId("question-option-q2-api"));
    expect(submit).toBeDisabled(); // q3 still unanswered

    fireEvent.change(screen.getByTestId("question-text-q3"), { target: { value: "  spaced  " } });
    expect(submit).toBeEnabled();
  });

  it("an Other answer alone satisfies a single question, and whitespace-only text does not", () => {
    const pending = pendingQuestion({
      request: { source: "agent", questions: [{ id: "q1", text: "Which?", kind: "single", options: [{ id: "a", label: "A" }], allowOther: true }] },
    });
    render(<QuestionCard runId="run-1" pending={pending} onAnswer={vi.fn()} />);
    expect(screen.getByTestId("question-submit")).toBeDisabled();

    fireEvent.change(screen.getByTestId("question-other-q1"), { target: { value: "   " } });
    expect(screen.getByTestId("question-submit")).toBeDisabled();

    fireEvent.change(screen.getByTestId("question-other-q1"), { target: { value: "the third one" } });
    expect(screen.getByTestId("question-submit")).toBeEnabled();
  });

  it("submits the payload for single, multi (with other text) and text answers", async () => {
    const onAnswer = vi.fn().mockResolvedValue(undefined);
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={onAnswer} />);

    answerAll(screen.getByTestId("question-card"));
    fireEvent.change(screen.getByTestId("question-other-q2"), { target: { value: "  cron jobs  " } });
    fireEvent.click(screen.getByTestId("question-submit"));
    await flush();

    expect(onAnswer).toHaveBeenCalledTimes(1);
    const [runId, requestId, answer] = onAnswer.mock.calls[0]! as [string, string, QuestionAnswer];
    expect(runId).toBe("run-1");
    expect(requestId).toBe("q-req-1");
    expect(answer).toEqual({
      action: "answer",
      answers: [
        { questionId: "q1", optionIds: ["prod"] },
        { questionId: "q2", optionIds: ["api"], text: "cron jobs" },
        { questionId: "q3", text: "thanks" },
      ],
    });
  });

  it("typing an Other answer deselects a single question's radio and vice versa", () => {
    const pending = pendingQuestion({
      request: { source: "agent", questions: [{ id: "q1", text: "Which?", kind: "single", options: [{ id: "a", label: "A" }], allowOther: true }] },
    });
    render(<QuestionCard runId="run-1" pending={pending} onAnswer={vi.fn()} />);

    fireEvent.click(screen.getByTestId("question-option-q1-a"));
    expect(screen.getByTestId("question-option-q1-a")).toBeChecked();

    fireEvent.change(screen.getByTestId("question-other-q1"), { target: { value: "something else" } });
    expect(screen.getByTestId("question-option-q1-a")).not.toBeChecked();
    expect(screen.getByTestId("question-submit")).toBeEnabled();

    fireEvent.click(screen.getByTestId("question-option-q1-a"));
    expect(screen.getByTestId("question-other-q1")).toHaveValue("");
    expect(screen.getByTestId("question-option-q1-a")).toBeChecked();
  });

  it("declines with the trimmed note, or with no note field at all", async () => {
    const withNote = vi.fn().mockResolvedValue(undefined);
    const first = render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={withNote} />);
    fireEvent.change(screen.getByTestId("question-decline-note"), { target: { value: "  not today  " } });
    fireEvent.click(screen.getByTestId("question-decline"));
    await flush();
    expect(withNote).toHaveBeenCalledTimes(1);
    expect(withNote.mock.calls[0]![2]).toEqual({ action: "decline", note: "not today" });
    first.unmount();

    // A fresh card (the answered one unmounts): a decline without a note sends no note field.
    const withoutNote = vi.fn().mockResolvedValue(undefined);
    render(<QuestionCard runId="run-2" pending={pendingQuestion()} onAnswer={withoutNote} />);
    fireEvent.click(screen.getByTestId("question-decline"));
    await flush();
    expect(withoutNote.mock.calls[0]![2]).toEqual({ action: "decline" });
  });

  it("caps answer and note input lengths", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    expect(screen.getByTestId("question-text-q3")).toHaveAttribute("maxlength", "2000");
    expect(screen.getByTestId("question-other-q2")).toHaveAttribute("maxlength", "2000");
    expect(screen.getByTestId("question-decline-note")).toHaveAttribute("maxlength", "500");
  });
});

describe("QuestionCard: in-flight and failure states", () => {
  it("disables both buttons while the call is in flight", async () => {
    const onAnswer = vi.fn(() => new Promise<void>(() => {}));
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={onAnswer} />);

    answerAll(screen.getByTestId("question-card"));
    fireEvent.click(screen.getByTestId("question-submit"));
    await flush();

    expect(screen.getByTestId("question-submit")).toBeDisabled();
    expect(screen.getByTestId("question-decline")).toBeDisabled();
  });

  it("shows a rejected call's error and keeps the card answerable", async () => {
    const onAnswer = vi.fn().mockRejectedValue(new Error("boom"));
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={onAnswer} />);

    answerAll(screen.getByTestId("question-card"));
    fireEvent.click(screen.getByTestId("question-submit"));
    await flush();

    const card = screen.getByTestId("question-card");
    expect(within(card).getByTestId("question-error")).toHaveTextContent("boom");
    expect(within(card).getByTestId("question-submit")).toBeEnabled();
    expect(within(card).getByTestId("question-decline")).toBeEnabled();
  });
});
