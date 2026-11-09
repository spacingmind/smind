import { useId, useLayoutEffect, useRef } from "react";

import { clampRunes, denyReasonCounter } from "@/lib/deny-reason";
import { cn } from "@/lib/utils";

/** Three text lines at the field's line height plus its padding: past this the box scrolls instead of growing. */
const MAX_FIELD_HEIGHT_PX = 84;

/**
 * The optional reason a human types with a Deny: a labelled textarea that starts at two lines and grows to three, capped at
 * the daemon's limit counted in runes (not UTF-16 units), with a character counter from near the limit on. Text only.
 */
export function DenyReasonField({
  value,
  onChange,
  disabled,
  testId = "permission-deny-reason",
}: {
  value: string;
  onChange: (next: string) => void;
  disabled?: boolean;
  testId?: string;
}) {
  const id = useId();
  const counterId = `${id}-counter`;
  const ref = useRef<HTMLTextAreaElement>(null);
  const counter = denyReasonCounter(value);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_FIELD_HEIGHT_PX)}px`;
    el.style.overflowY =
      el.scrollHeight > MAX_FIELD_HEIGHT_PX ? "auto" : "hidden";
  }, [value]);

  return (
    <div className="flex w-full flex-col gap-1">
      <label htmlFor={id} className="text-ui-xs text-foreground-muted">
        Reason for denying (optional)
      </label>
      <textarea
        ref={ref}
        id={id}
        rows={2}
        data-testid={testId}
        placeholder="Tell the agent what to do instead"
        value={value}
        disabled={disabled}
        aria-describedby={counter.shown ? counterId : undefined}
        onChange={(e) => onChange(clampRunes(e.target.value))}
        className={cn(
          "w-full min-w-0 resize-none rounded-lg border border-input-border bg-input px-2.5 py-1.5 text-ui-base leading-5 text-foreground outline-none transition-colors",
          "placeholder:text-foreground-subtlest hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:cursor-not-allowed disabled:opacity-50",
        )}
      />
      {counter.shown && (
        <div
          id={counterId}
          data-testid={`${testId}-counter`}
          className={cn(
            "self-end text-ui-xs tabular-nums",
            counter.atLimit
              ? "font-medium text-destructive"
              : "text-foreground-muted",
          )}
        >
          {counter.used} / {counter.max}
          {counter.atLimit ? " — limit reached" : ""}
        </div>
      )}
    </div>
  );
}
