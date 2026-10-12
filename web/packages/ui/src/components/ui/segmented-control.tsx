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
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  /** Accessible name of the whole group. */
  label: string;
  className?: string;
}) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      aria-label={label}
      onValueChange={(next) => {
        // Radix reports "" when the pressed segment is clicked again; a
        // segmented control always has a selection.
        if (next) onChange(next as T);
      }}
      className={cn("inline-flex rounded-lg border border-input-border p-0.5", className)}
    >
      {options.map((opt) => (
        <ToggleGroup.Item
          key={opt.value}
          value={opt.value}
          className={cn(
            "h-7 rounded-md px-3 text-ui-sm text-muted-foreground outline-none transition-colors",
            "hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
            "data-[state=on]:bg-selected data-[state=on]:text-foreground",
          )}
        >
          {opt.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  );
}
