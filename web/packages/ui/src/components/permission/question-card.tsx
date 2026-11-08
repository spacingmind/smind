import { useEffect, useId, useRef, useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { COMPACT_TOUCH_BUTTON_CLASS } from "@/components/permission/permission-option-button";
import type { PendingQuestion } from "@/hooks/use-run-timeline";
import type { Question, QuestionAnswer, QuestionAnswerItem } from "@/lib/types";

/** Free-text answer cap (ADR-0055): the daemon enforces it too; maxLength keeps the client honest before the round trip. */
const MAX_ANSWER_CHARS = 2000;
/** Cap for the decline note. */
const MAX_NOTE_CHARS = 500;

/** A plain <textarea> wearing Input's own field styling (components/ui has no Textarea). */
const TEXTAREA_CLASS =
  "w-full min-w-0 resize-y rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-lg transition-colors outline-none placeholder:text-foreground-subtlest hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-ui-base";

/** True when the currently focused element is a text input the user could be mid-keystroke in -- the composer's textarea, chiefly. */
function isTextEntryFocused(): boolean {
  const active = document.activeElement;
  if (!(active instanceof HTMLElement)) return false;
  return active.tagName === "TEXTAREA" || active.tagName === "INPUT" || active.isContentEditable;
}

/** One question's in-progress answer: the selected option id(s), the "Other" free text, and the "text" kind's answer. */
interface Draft {
  optionIds: string[];
  otherText: string;
  text: string;
}

const EMPTY_DRAFT: Draft = { optionIds: [], otherText: "", text: "" };

/** Whether this question currently has a submittable answer (ADR-0055 §7: never preselect, never guess). */
function isAnswered(question: Question, draft: Draft): boolean {
  if (question.kind === "text") return draft.text.trim() !== "";
  if (question.kind === "single") return draft.optionIds.length > 0 || draft.otherText.trim() !== "";
  return draft.optionIds.length > 0 || draft.otherText.trim() !== "";
}

/** The answers array a Submit sends: one item per question, empty fields omitted. */
export function buildAnswers(questions: Question[], drafts: Record<string, Draft>): QuestionAnswerItem[] {
  return questions.map((question) => {
    const draft = drafts[question.id] ?? EMPTY_DRAFT;
    const item: QuestionAnswerItem = { questionId: question.id };
    if (draft.optionIds.length > 0) item.optionIds = draft.optionIds;
    const text = (question.kind === "text" ? draft.text : draft.otherText).trim();
    if (text !== "") item.text = text;
    return item;
  });
}

/**
 * The agent-question card (ADR-0055): the structured ask an agent routes
 * to the human, answered via run.answerQuestion. Same dock placement,
 * look and focus behaviour as permission-card (one blocking ask, one
 * card), but a form: one fieldset per question -- radio group ("single"),
 * checkbox group ("multi") or textarea ("text"), each option with its
 * description, and an "Other" free-text line when the question allows
 * one.
 *
 * Security (ADR-0055 §7): every agent-authored string -- header, question
 * text, option labels and descriptions -- renders as a plain React text
 * node (whitespace-pre-wrap break-words), never markdown, never a link,
 * never HTML. Nothing is ever preselected: an agent cannot phrase a
 * question whose "default" answer is already chosen.
 */
export function QuestionCard({
  runId,
  pending,
  onAnswer,
}: {
  runId: string;
  pending: PendingQuestion;
  onAnswer: (runId: string, requestId: string, answer: QuestionAnswer) => Promise<void>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A new requestId is a whole new ask: task-detail keys this card by
  // requestId, so it remounts (drafts and note discarded) rather than
  // mutating in place.
  useEffect(() => {
    if (isTextEntryFocused()) return;
    ref.current?.focus();
  }, [pending.requestId]);

  const questions = pending.request.questions;
  const allAnswered = questions.every((q) => isAnswered(q, drafts[q.id] ?? EMPTY_DRAFT));

  function setDraft(questionId: string, next: Draft) {
    setDrafts((prev) => ({ ...prev, [questionId]: next }));
  }

  async function submit(answer: QuestionAnswer) {
    setSubmitting(true);
    setError(null);
    try {
      await onAnswer(runId, pending.requestId, answer);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSubmitting(false);
    }
    // On success, stay disabled: the run's own subscription observes the
    // matching "question_resolved" event and the card unmounts when
    // pendingQuestion clears -- same rule as options-card.
  }

  const trimmedNote = note.trim();

  return (
    <div
      ref={ref}
      tabIndex={-1}
      role="group"
      aria-label={questions[0] ? `Agent question: ${questions[0].text}` : "Agent question"}
      data-testid="question-card"
      className="flex flex-col gap-2 rounded-2xl bg-card p-2 shadow-lg outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      {/* Amber like the composer's "Waiting for you" chip (an attention state), not the permission card's green (a documented parity rule that card keeps). */}
      <span
        data-testid="question-waiting-badge"
        className="inline-flex w-fit items-center gap-1.5 rounded-full bg-warning/10 px-2 py-0.5 text-ui-xs font-medium text-warning-text"
      >
        <span aria-hidden className="size-2 rounded-full bg-warning" />
        Waiting for your response
      </span>
      {pending.request.truncated && <p className="text-ui-xs text-foreground-muted">Some of this text was shortened.</p>}
      {error && <Alert variant="error" testId="question-error" description={error} />}
      {/* A long ask scrolls inside the card, so Submit and Decline stay on screen (phones). */}
      <div data-testid="question-fields" className="flex max-h-[50vh] flex-col gap-2 overflow-y-auto">
        {questions.map((question) => (
          <QuestionField
            key={question.id}
            question={question}
            draft={drafts[question.id] ?? EMPTY_DRAFT}
            disabled={submitting}
            onChange={(next) => setDraft(question.id, next)}
          />
        ))}
      </div>
      <div className="flex flex-col gap-2">
        <Input
          data-testid="question-decline-note"
          aria-label="Note sent with a decline (optional)"
          placeholder="Note for the agent (optional)"
          value={note}
          maxLength={MAX_NOTE_CHARS}
          disabled={submitting}
          onChange={(e) => setNote(e.target.value)}
        />
        {!submitting && !allAnswered && (
          <p data-testid="question-submit-hint" className="text-ui-xs text-foreground-muted">
            Answer every question to submit.
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={submitting || !allAnswered}
            data-testid="question-submit"
            onClick={() => submit({ action: "answer", answers: buildAnswers(questions, drafts) })}
          >
            Submit
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={submitting}
            data-testid="question-decline"
            onClick={() => submit({ action: "decline", ...(trimmedNote !== "" ? { note: trimmedNote } : {}) })}
          >
            Decline
          </Button>
        </div>
      </div>
    </div>
  );
}

function QuestionField({
  question,
  draft,
  disabled,
  onChange,
}: {
  question: Question;
  draft: Draft;
  disabled: boolean;
  onChange: (next: Draft) => void;
}) {
  const fieldId = useId();
  const otherId = useId();
  const [expanded, setExpanded] = useState(false);
  const options = question.options ?? [];
  const isChoice = question.kind === "single" || question.kind === "multi";
  // Long asks collapse visually (the card stays scannable); the clamp is CSS
  // only, so the legend still carries the fieldset's full accessible name.
  const isLong = question.text.length > 480 || question.text.split("\n").length > 8;

  function toggleOption(optionId: string) {
    if (question.kind === "single") {
      // Selecting an option is the "vice versa" half of the Other/radio
      // exclusion: a picked option clears any Other text.
      onChange({ ...draft, optionIds: [optionId], otherText: "" });
      return;
    }
    const selected = draft.optionIds.includes(optionId);
    onChange({
      ...draft,
      optionIds: selected ? draft.optionIds.filter((id) => id !== optionId) : [...draft.optionIds, optionId],
    });
  }

  return (
    <fieldset data-testid="question-field" className="flex flex-col gap-1.5">
      {isLong ? (
        <>
          <legend>
            <span className={`block whitespace-pre-wrap break-words${expanded ? "" : " line-clamp-8"}`}>{question.text}</span>
          </legend>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-testid="question-text-toggle"
            aria-expanded={expanded}
            disabled={disabled}
            onClick={() => setExpanded((value) => !value)}
            className="self-start"
          >
            {expanded ? "Show less" : "Show all"}
          </Button>
        </>
      ) : (
        <legend className="whitespace-pre-wrap break-words">{question.text}</legend>
      )}
      {question.header && (
        <span className="inline-flex w-fit rounded-full border px-2 py-0.5 text-ui-xs text-foreground-muted">{question.header}</span>
      )}

      {isChoice && (
        <div className="flex flex-col gap-1">
          {options.map((option) => (
            <label key={option.id} className="flex flex-col gap-0.5">
              <span className="flex items-start gap-2">
                <input
                  type={question.kind === "multi" ? "checkbox" : "radio"}
                  name={`${fieldId}-options`}
                  checked={draft.optionIds.includes(option.id)}
                  disabled={disabled}
                  data-testid={`question-option-${question.id}-${option.id}`}
                  className="mt-0.5 size-4 shrink-0"
                  onChange={() => toggleOption(option.id)}
                />
                <span className="whitespace-pre-wrap break-words">{option.label}</span>
              </span>
              {option.description && (
                <span className="pl-6 text-ui-sm text-foreground-muted whitespace-pre-wrap break-words">{option.description}</span>
              )}
            </label>
          ))}
        </div>
      )}

      {question.kind === "text" ? (
        <textarea
          data-testid={`question-text-${question.id}`}
          className={TEXTAREA_CLASS}
          rows={3}
          placeholder="Type your answer…"
          value={draft.text}
          maxLength={MAX_ANSWER_CHARS}
          disabled={disabled}
          onChange={(e) => onChange({ ...draft, text: e.target.value })}
        />
      ) : (
        question.allowOther && (
          <div className="flex flex-col gap-1">
            {/* A visible label: once the field holds a value, "Other…"-alone no longer names it. */}
            <label htmlFor={otherId} className="text-ui-sm text-foreground-muted">
              Other:
            </label>
            <Input
              id={otherId}
              data-testid={`question-other-${question.id}`}
              placeholder="Type another answer…"
              value={draft.otherText}
              maxLength={MAX_ANSWER_CHARS}
              disabled={disabled}
              onChange={(e) =>
                // Typing an Other answer deselects a single question's radio
                // (multi keeps its checkboxes -- Other is just one more choice).
                onChange({
                  ...draft,
                  otherText: e.target.value,
                  optionIds: question.kind === "single" && e.target.value !== "" ? [] : draft.optionIds,
                })
              }
            />
          </div>
        )
      )}
    </fieldset>
  );
}
