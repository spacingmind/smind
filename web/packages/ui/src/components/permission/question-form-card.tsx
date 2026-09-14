import { useId, useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { PendingPermission } from "@/hooks/use-run-timeline";
import type { PermissionQuestion } from "@/lib/types";

/** One question's in-progress answer: the selected option id(s), and/or free text for "other"/free_text questions. */
export interface QuestionAnswer {
  optionIds: string[];
  otherText: string;
}

function emptyAnswer(): QuestionAnswer {
  return { optionIds: [], otherText: "" };
}

/**
 * The batch this form submits, encoded into the single `optionId` string
 * `run.respondPermission` accepts. **No daemon path parses this today**
 * (see PermissionQuestion's doc comment in lib/types.ts) -- it is the
 * client's own placeholder encoding, kept obviously structured (a tagged
 * JSON envelope, not a delimited string) so a future daemon-side consumer
 * has something unambiguous to read rather than a format frozen by
 * accident.
 */
export function encodeAnswerBatch(answers: Record<string, QuestionAnswer>): string {
  return JSON.stringify({ kind: "question_form_answers", answers });
}

/** The blank shape "skip" sends: every question present, every answer empty. */
export function blankAnswerBatch(questions: PermissionQuestion[]): string {
  const answers: Record<string, QuestionAnswer> = {};
  for (const q of questions) answers[q.id] = emptyAnswer();
  return encodeAnswerBatch(answers);
}

/**
 * The structured question-form variant (Item 11, per
 * `audit-deepseek-harness.md` §2): one or more questions, each
 * single-select, multi-select, or free-text, with an optional "other"
 * free-text answer and a "skip" escape hatch that submits the blank
 * shape rather than blocking the run forever.
 */
export function QuestionFormCard({
  runId,
  pending,
  onRespond,
}: {
  runId: string;
  pending: PendingPermission & { questions: PermissionQuestion[] };
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
}) {
  const [answers, setAnswers] = useState<Record<string, QuestionAnswer>>(() => {
    const initial: Record<string, QuestionAnswer> = {};
    for (const q of pending.questions) initial[q.id] = emptyAnswer();
    return initial;
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function setAnswer(questionId: string, next: QuestionAnswer) {
    setAnswers((prev) => ({ ...prev, [questionId]: next }));
  }

  async function submit(batch: string) {
    setSubmitting(true);
    setError(null);
    try {
      await onRespond(runId, pending.requestId, batch);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSubmitting(false);
    }
  }

  return (
    <Alert testId="pending-permission" variant="warning" title={pending.summary} description={error ?? undefined}>
      <div className="flex w-full flex-col gap-3" data-testid="question-form">
        {pending.questions.map((question) => (
          <QuestionField
            key={question.id}
            question={question}
            answer={answers[question.id] ?? emptyAnswer()}
            disabled={submitting}
            onChange={(next) => setAnswer(question.id, next)}
          />
        ))}
        <div className="flex items-center gap-2">
          <Button
            type="button"
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={submitting}
            data-testid="question-form-submit"
            onClick={() => submit(encodeAnswerBatch(answers))}
          >
            Submit
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={submitting}
            data-testid="question-form-skip"
            onClick={() => submit(blankAnswerBatch(pending.questions))}
          >
            Skip
          </Button>
        </div>
      </div>
    </Alert>
  );
}

function QuestionField({
  question,
  answer,
  disabled,
  onChange,
}: {
  question: PermissionQuestion;
  answer: QuestionAnswer;
  disabled: boolean;
  onChange: (next: QuestionAnswer) => void;
}) {
  const fieldId = useId();
  const options = question.options ?? [];

  function toggleOption(optionId: string) {
    if (question.kind === "single_select") {
      onChange({ ...answer, optionIds: [optionId] });
      return;
    }
    const selected = answer.optionIds.includes(optionId);
    onChange({
      ...answer,
      optionIds: selected ? answer.optionIds.filter((id) => id !== optionId) : [...answer.optionIds, optionId],
    });
  }

  return (
    <fieldset data-testid="question-field" className="flex flex-col gap-1">
      <legend className="text-xs font-medium">{question.prompt}</legend>

      {question.kind !== "free_text" && (
        <div className="flex flex-wrap gap-1.5" role="group" aria-label={question.prompt}>
          {options.map((option) => {
            const selected = answer.optionIds.includes(option.id);
            return (
              <Button
                key={option.id}
                type="button"
                variant={selected ? "default" : "outline"}
                size="sm"
                className="h-6 px-2 text-xs"
                disabled={disabled}
                aria-pressed={selected}
                data-testid={`question-option-${option.id}`}
                onClick={() => toggleOption(option.id)}
              >
                {option.label}
              </Button>
            );
          })}
        </div>
      )}

      {(question.kind === "free_text" || question.allowOther) && (
        <label htmlFor={fieldId} className="sr-only">
          {question.kind === "free_text" ? question.prompt : "Other"}
        </label>
      )}
      {(question.kind === "free_text" || question.allowOther) && (
        <Input
          id={fieldId}
          data-testid="question-other-input"
          placeholder={question.kind === "free_text" ? "Type your answer…" : "Other…"}
          value={answer.otherText}
          disabled={disabled}
          onChange={(e) => onChange({ ...answer, otherText: e.target.value })}
        />
      )}
    </fieldset>
  );
}
