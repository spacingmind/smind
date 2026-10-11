import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * A row of mutually-exclusive step buttons -- the same shape for the
 * theme picker, each font-size axis and the Usage section's controls, so
 * those read as one control family rather than different widget kinds.
 */
export function StepGroup<T extends string>({
  options,
  value,
  onChange,
  groupLabel,
  isDisabled,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  groupLabel: string;
  isDisabled?: (value: T) => boolean;
}) {
  return (
    <div role="group" aria-label={groupLabel} className="inline-flex rounded-lg border border-input-border p-0.5">
      {options.map((opt) => (
        <Button
          key={opt.value}
          type="button"
          variant="ghost"
          size="sm"
          aria-pressed={opt.value === value}
          disabled={isDisabled?.(opt.value)}
          data-testid={`settings-${groupLabel}-${opt.value}`}
          onClick={() => onChange(opt.value)}
          className={cn("h-7 rounded-md px-3 text-ui-sm", opt.value === value && "bg-selected")}
        >
          {opt.label}
        </Button>
      ))}
    </div>
  );
}
