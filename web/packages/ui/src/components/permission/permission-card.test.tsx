import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { blankAnswerBatch, encodeAnswerBatch } from "@/components/permission/question-form-card";
import { optionVariant, recommendedOptionId } from "@/components/permission/permission-option-button";
import { PermissionCard } from "@/components/permission/permission-card";
import { PLAN_REVIEW_APPROVE, PLAN_REVIEW_REFUSE } from "@/components/permission/plan-review-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";

function pending(overrides: Partial<PendingPermission> = {}): PendingPermission {
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

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe("optionVariant / recommendedOptionId", () => {
  it("styles reject kinds as destructive regardless of position", () => {
    expect(optionVariant("reject_once", false)).toBe("destructive");
    expect(optionVariant("reject_always", true)).toBe("destructive");
  });

  it("styles the first allow-kind option as the recommended (primary) action", () => {
    const options = [
      { id: "a", label: "Deny", kind: "reject_once" },
      { id: "b", label: "Allow once", kind: "allow_once" },
      { id: "c", label: "Allow always", kind: "allow_always" },
    ];
    expect(recommendedOptionId(options)).toBe("b");
    expect(optionVariant("allow_once", true)).toBe("default");
    expect(optionVariant("allow_always", false)).toBe("outline");
  });
});

describe("PermissionCard: options variant", () => {
  it("renders every option and styles allow/deny distinctly", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);

    const card = screen.getByTestId("pending-permission");
    const allow = within(card).getByRole("button", { name: "Allow" });
    const deny = within(card).getByRole("button", { name: "Deny" });
    expect(allow).toHaveAttribute("data-option-kind", "allow_once");
    expect(deny).toHaveAttribute("data-option-kind", "reject_once");
    expect(allow.className).not.toBe(deny.className);
  });

  it("renders every option even when no kind is recognised", () => {
    render(
      <PermissionCard
        runId="run-1"
        pending={pending({ options: [{ id: "x", label: "Do it", kind: "" }] })}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Do it" })).toBeInTheDocument();
  });

  it("is focusable as a group and moves focus to itself when a new request arrives", () => {
    const { rerender } = render(
      <PermissionCard runId="run-1" pending={pending({ requestId: "req-1" })} onRespond={vi.fn()} onChat={vi.fn()} />,
    );
    const group = screen.getByRole("group", { name: "Run a risky command" });
    expect(group).toHaveAttribute("tabindex", "-1");
    expect(document.activeElement).toBe(group);

    // A second, different request re-focuses the card.
    (group as HTMLElement).blur();
    rerender(
      <PermissionCard
        runId="run-1"
        pending={pending({ requestId: "req-2", summary: "Run another risky command" })}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(document.activeElement).toBe(screen.getByRole("group", { name: "Run another risky command" }));
  });

  it("does not steal focus from a text field the user is actively typing in when a new request arrives", () => {
    function Harness({ requestId }: { requestId: string }) {
      return (
        <>
          <textarea aria-label="composer" />
          <PermissionCard runId="run-1" pending={pending({ requestId })} onRespond={vi.fn()} onChat={vi.fn()} />
        </>
      );
    }

    const { rerender } = render(<Harness requestId="req-1" />);
    const composer = screen.getByLabelText("composer");
    composer.focus();
    expect(document.activeElement).toBe(composer);

    // A second, different request must not yank focus off the field the
    // user is mid-keystroke in.
    rerender(<Harness requestId="req-2" />);
    expect(document.activeElement).toBe(composer);
  });

  it("options are real buttons, reachable in tab order and activatable by click", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);

    const allow = screen.getByRole("button", { name: "Allow" });
    expect(allow.tagName).toBe("BUTTON");
    expect(allow).not.toHaveAttribute("tabindex", "-1");

    allow.focus();
    expect(document.activeElement).toBe(allow);
    fireEvent.click(allow);
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "allow-1");
  });
});

describe("PermissionCard: question-form variant", () => {
  const withQuestions = pending({
    questions: [
      { id: "q1", prompt: "Which environment?", kind: "single_select", options: [{ id: "prod", label: "Prod" }, { id: "staging", label: "Staging" }] },
      { id: "q2", prompt: "Anything else?", kind: "free_text" },
    ],
  });

  it("renders the question form instead of a plain option list", () => {
    render(<PermissionCard runId="run-1" pending={withQuestions} onRespond={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByTestId("question-form")).toBeInTheDocument();
    expect(screen.getAllByTestId("question-field")).toHaveLength(2);
    expect(screen.getByText("Which environment?")).toBeInTheDocument();
  });

  it("submits one structured answer batch for both selections and free text", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={withQuestions} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.click(screen.getByTestId("question-option-prod"));
    fireEvent.change(screen.getByTestId("question-other-input"), { target: { value: "nothing" } });
    fireEvent.click(screen.getByTestId("question-form-submit"));
    await flush();

    expect(onRespond).toHaveBeenCalledTimes(1);
    const [runId, requestId, batch] = onRespond.mock.calls[0]!;
    expect(runId).toBe("run-1");
    expect(requestId).toBe("req-1");
    expect(JSON.parse(batch)).toEqual({
      kind: "question_form_answers",
      answers: {
        q1: { optionIds: ["prod"], otherText: "" },
        q2: { optionIds: [], otherText: "nothing" },
      },
    });
  });

  it("skip sends the blank shape", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={withQuestions} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.click(screen.getByTestId("question-form-skip"));
    await flush();

    const [, , batch] = onRespond.mock.calls[0]!;
    expect(batch).toBe(blankAnswerBatch(withQuestions.questions!));
  });

  it("single_select keeps only the latest choice; multi_select accumulates", () => {
    const multi = pending({
      questions: [{ id: "q1", prompt: "Pick any", kind: "multi_select", options: [{ id: "a", label: "A" }, { id: "b", label: "B" }] }],
    });
    render(<PermissionCard runId="run-1" pending={multi} onRespond={vi.fn()} onChat={vi.fn()} />);

    fireEvent.click(screen.getByTestId("question-option-a"));
    fireEvent.click(screen.getByTestId("question-option-b"));
    expect(screen.getByTestId("question-option-a")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("question-option-b")).toHaveAttribute("aria-pressed", "true");
  });
});

describe("PermissionCard: plan-review variant", () => {
  const withPlan = pending({ plan: "# Do the thing\n\n- step one\n- step two" });

  it("renders the plan as markdown with the three actions", () => {
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={vi.fn()} onChat={vi.fn()} />);

    expect(within(screen.getByTestId("plan-review")).getByRole("heading", { level: 1 })).toHaveTextContent("Do the thing");
    expect(screen.getByTestId("plan-review-chat")).toBeInTheDocument();
    expect(screen.getByTestId("plan-review-refuse")).toBeInTheDocument();
    expect(screen.getByTestId("plan-review-approve")).toBeInTheDocument();
  });

  it("approve/refuse resolve the permission; chat only calls back without resolving", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    const onChat = vi.fn();
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={onRespond} onChat={onChat} />);

    fireEvent.click(screen.getByTestId("plan-review-chat"));
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onRespond).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("plan-review-approve"));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", PLAN_REVIEW_APPROVE);
  });

  it("refuse sends the refuse option", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.click(screen.getByTestId("plan-review-refuse"));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", PLAN_REVIEW_REFUSE);
  });

  it("a plan shape wins over a questions shape carried on the same request", () => {
    render(
      <PermissionCard
        runId="run-1"
        pending={{ ...withPlan, questions: [{ id: "q1", prompt: "ignored", kind: "free_text" }] }}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(screen.getByTestId("plan-review")).toBeInTheDocument();
    expect(screen.queryByTestId("question-form")).not.toBeInTheDocument();
  });
});

describe("encodeAnswerBatch", () => {
  it("is a tagged JSON envelope, not an ad hoc string format", () => {
    const encoded = encodeAnswerBatch({ q1: { optionIds: ["a"], otherText: "" } });
    expect(JSON.parse(encoded)).toEqual({ kind: "question_form_answers", answers: { q1: { optionIds: ["a"], otherText: "" } } });
  });
});
