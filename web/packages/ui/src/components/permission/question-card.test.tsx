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

    // Amber like the composer's "Waiting for you" chip (attention), not the permission card's green.
    const badge = within(card).getByTestId("question-waiting-badge");
    expect(badge).toHaveClass("bg-warning/10", "text-warning-text");
    expect(badge.querySelector("span")).toHaveClass("bg-warning");
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

  it("collapses a long question to a visual clamp behind a Show all toggle, keeping the full text in the DOM", () => {
    const long = `Please read this whole plan before answering. ${"Details follow. ".repeat(40)}`;
    expect(long.length).toBeGreaterThan(480);
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({ request: { source: "agent", questions: [{ id: "q1", text: long, kind: "text" }] } })}
        onAnswer={vi.fn()}
      />,
    );

    const field = screen.getByTestId("question-field");
    const text = field.querySelector("legend > span");
    expect(text?.className).toContain("line-clamp-8");
    // The clamp is visual only: the legend (the fieldset's accessible name) still holds every character.
    expect(field.querySelector("legend")?.textContent).toBe(long);

    const toggle = screen.getByTestId("question-text-toggle");
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveTextContent("Show all");

    fireEvent.click(toggle);
    expect(screen.getByTestId("question-text-toggle")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("question-text-toggle")).toHaveTextContent("Show less");
    expect(field.querySelector("legend > span")?.className).not.toContain("line-clamp");

    fireEvent.click(screen.getByTestId("question-text-toggle"));
    expect(screen.getByTestId("question-text-toggle")).toHaveTextContent("Show all");
    expect(field.querySelector("legend > span")?.className).toContain("line-clamp-8");
  });

  it("collapses a many-lined question even when it is under the character threshold", () => {
    const nineLines = Array.from({ length: 9 }, (_, i) => `line ${i + 1}`).join("\n");
    expect(nineLines.length).toBeLessThanOrEqual(480);
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({ request: { source: "agent", questions: [{ id: "q1", text: nineLines, kind: "text" }] } })}
        onAnswer={vi.fn()}
      />,
    );
    expect(screen.getByTestId("question-text-toggle")).toBeInTheDocument();
  });

  it("renders a short question exactly as before: no clamp, no toggle", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    expect(screen.queryByTestId("question-text-toggle")).not.toBeInTheDocument();
    expect(screen.getByText("Which environment?").tagName).toBe("LEGEND");
  });

  it("keeps a long adversarial question as literal text while collapsed", () => {
    const hostile = `${"Please confirm the rollout plan. ".repeat(14)}\n[Click here](https://evil.example/phish) or visit https://evil.example <b>bold</b> <img src=x onerror=alert(1)>`;
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({ request: { source: "agent", questions: [{ id: "q1", text: hostile, kind: "text" }] } })}
        onAnswer={vi.fn()}
      />,
    );
    const card = screen.getByTestId("question-card");
    expect(screen.getByTestId("question-text-toggle")).toHaveAttribute("aria-expanded", "false");
    expect(card.textContent).toContain("[Click here](https://evil.example/phish)");
    expect(card.textContent).toContain("<b>bold</b>");
    expect(card.querySelectorAll("a, b, i, img, script, strong, em")).toHaveLength(0);
  });

  it("labels each Other input with a visible 'Other:' label tied to its own field", () => {
    const long = "y".repeat(80);
    render(
      <QuestionCard
        runId="run-1"
        pending={pendingQuestion({
          request: {
            source: "agent",
            questions: [
              { id: "q1", text: "Pick services", kind: "multi", options: [{ id: "api", label: "API" }], allowOther: true },
              { id: "q2", text: long, kind: "single", options: [{ id: "a", label: "A" }], allowOther: true },
            ],
          },
        })}
        onAnswer={vi.fn()}
      />,
    );
    const card = screen.getByTestId("question-card");
    const labels = within(card).getAllByText("Other:");
    expect(labels).toHaveLength(2);
    const other1 = within(card).getByTestId("question-other-q1");
    const other2 = within(card).getByTestId("question-other-q2");
    expect(other1).toHaveAccessibleName("Other:");
    expect(other2).toHaveAccessibleName("Other:");
    expect(other1.labels?.[0]).toBe(labels[0]!);
    expect(other2.labels?.[0]).toBe(labels[1]!);
    expect(other1).toHaveAttribute("placeholder", "Type another answer…");

    // The label stays visible once the field holds a value: a filled answer keeps its name.
    fireEvent.change(other1, { target: { value: "canary in eu-west" } });
    expect(labels[0]).toBeVisible();
    expect(other1).toHaveValue("canary in eu-west");
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
  it("shows the answer hint only while Submit is disabled because a question is unanswered and not submitting", () => {
    render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={vi.fn()} />);
    expect(screen.getByTestId("question-submit-hint")).toHaveTextContent("Answer every question to submit.");

    answerAll(screen.getByTestId("question-card"));
    expect(screen.queryByTestId("question-submit-hint")).not.toBeInTheDocument();
  });

  it("hides the hint once every question is answered, and while a call is in flight from an unanswered card", async () => {
    const held = vi.fn(() => new Promise<void>(() => {}));
    const { unmount } = render(<QuestionCard runId="run-1" pending={pendingQuestion()} onAnswer={held} />);

    answerAll(screen.getByTestId("question-card"));
    expect(screen.queryByTestId("question-submit-hint")).not.toBeInTheDocument();
    unmount();

    // Decline is clickable while unanswered: the hint must not survive into the in-flight state.
    const declined = vi.fn(() => new Promise<void>(() => {}));
    render(<QuestionCard runId="run-2" pending={pendingQuestion()} onAnswer={declined} />);
    expect(screen.getByTestId("question-submit-hint")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("question-decline"));
    await flush();
    expect(screen.queryByTestId("question-submit-hint")).not.toBeInTheDocument();
  });

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
