import { editorProblems, MAX_BODY_LINES, MAX_DESCRIPTION_CHARS, nonBlankLines } from "@/lib/learning-format";
import { FormError } from "@/components/ui/form-error";
import { cn } from "@/lib/utils";

const FIELD =
  "w-full resize-y rounded-lg border border-input-border bg-input px-2.5 py-1.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused";

function Counter({ value, max, unit, testId }: { value: number; max: number; unit: string; testId: string }) {
  return (
    <span data-testid={testId} className={cn("text-ui-sm tabular-nums", value > max ? "text-destructive" : "text-foreground-subtle")}>
      {value} / {max} {unit}
    </span>
  );
}

/**
 * Editable description (when given) and body with the limits the daemon's gate enforces, shown
 * as live counters: a one-sentence description of at most 160 characters, a body of at most 25
 * non-blank lines. The owner reads `editorProblems(...)` to decide whether it may submit.
 */
export function SkillEditor({
  description,
  body,
  onChange,
  idPrefix = "skill-editor",
  bodyLabel = "Body",
}: {
  /** Omit (undefined) to edit the body only. */
  description?: string;
  body: string;
  onChange: (next: { description?: string; body: string }) => void;
  idPrefix?: string;
  bodyLabel?: string;
}) {
  const problems = editorProblems({ description, body });
  return (
    <div className="flex flex-col gap-3" data-testid={idPrefix}>
      {description !== undefined && (
        <label className="flex flex-col gap-1">
          <span className="flex items-baseline justify-between gap-2 text-ui-sm text-foreground-muted">
            Description (one sentence)
            <Counter testId={`${idPrefix}-description-count`} value={description.trim().length} max={MAX_DESCRIPTION_CHARS} unit="chars" />
          </span>
          <textarea
            aria-label="Description"
            data-testid={`${idPrefix}-description`}
            rows={2}
            className={FIELD}
            aria-invalid={problems.description ? true : undefined}
            value={description}
            onChange={(e) => onChange({ description: e.target.value, body })}
          />
          {problems.description && <FormError message={problems.description} size="sm" />}
        </label>
      )}
      <label className="flex flex-col gap-1">
        <span className="flex items-baseline justify-between gap-2 text-ui-sm text-foreground-muted">
          {bodyLabel}
          <Counter testId={`${idPrefix}-body-count`} value={nonBlankLines(body)} max={MAX_BODY_LINES} unit="lines" />
        </span>
        <textarea
          aria-label={bodyLabel}
          data-testid={`${idPrefix}-body`}
          rows={10}
          className={cn(FIELD, "font-mono text-ui-sm")}
          aria-invalid={problems.body ? true : undefined}
          value={body}
          onChange={(e) => onChange({ description, body: e.target.value })}
        />
        {problems.body && <FormError message={problems.body} size="sm" />}
      </label>
    </div>
  );
}
