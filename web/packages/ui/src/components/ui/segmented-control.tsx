import type { ReactNode } from "react";
import { ToggleGroup } from "radix-ui";

import { cn } from "@/lib/utils";

/**
 * A small single-choice segmented control (Paseo's SegmentedControl, used
 * for Used / Remaining). Radix ToggleGroup in `single` mode, exposed as a
 * radio group: arrow keys move between segments, Space/Enter select, and a
 * click on the selected segment does not clear the choice.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  label,
  className,
  disabled,
  testId,
  instant = false,
}: {
  /** `icon` makes the segment icon-only: `label` becomes its aria-label (and tooltip). */
  options: { value: T; label: string; icon?: ReactNode; testId?: string }[];
  value: T;
  onChange: (value: T) => void;
  /** Accessible name of the whole group. */
  label: string;
  className?: string;
  disabled?: boolean;
  testId?: string;
  /** Selection changes without the colour fade, so text derived from the same value never leads the highlight. */
  instant?: boolean;
}) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      aria-label={label}
      disabled={disabled}
      data-testid={testId}
      onValueChange={(next) => {
        // Radix reports "" when the pressed segment is clicked again; a
        // segmented control always has a selection.
        if (next) onChange(next as T);
      }}
      className={cn("inline-flex rounded-lg border border-input-border p-0.5 data-[disabled]:opacity-50", className)}
    >
      {options.map((opt) => (
        <ToggleGroup.Item
          key={opt.value}
          value={opt.value}
          aria-label={opt.icon ? opt.label : undefined}
          title={opt.icon ? opt.label : undefined}
          data-testid={opt.testId}
          className={cn(
            "h-7 rounded-md px-3 text-ui-sm text-muted-foreground outline-none",
            !instant && "transition-colors",
            "hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
            "data-[state=on]:bg-selected data-[state=on]:text-foreground",
            opt.icon && "inline-flex items-center justify-center px-1.5",
          )}
        >
          {opt.icon ?? opt.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  );
}
