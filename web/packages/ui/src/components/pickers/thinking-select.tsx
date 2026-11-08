import { Icons } from "@/lib/icons";
import { pillTriggerClass } from "@/components/composer/pill-class";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { THINKING_LEVELS } from "@/lib/thinking-levels";

/** The shared thinking-level Select (Claude-only, see thinking-levels.ts): pill variant is the composer toolbar's trigger; field variant is a full-width form control. */
export function ThinkingSelect({
  levels = THINKING_LEVELS,
  value,
  onChange,
  disabled = false,
  variant,
  placeholder,
  testId,
  label = "Thinking level",
  title,
  showLabel = false,
}: {
  levels?: { id: string; label: string; help?: string }[];
  value: string;
  onChange: (levelId: string) => void;
  disabled?: boolean;
  variant: "pill" | "field";
  placeholder?: string;
  testId?: string;
  /** The accessible name; a live ACP session's own option names itself (`option.name`). */
  label?: string;
  /** Overrides the current level's help text as the tooltip (a live option's own description). */
  title?: string;
  /** Pill only: keep the label visible below sm instead of the icon-only trigger. */
  showLabel?: boolean;
}) {
  const current = levels.find((t) => t.id === value);
  return (
    // Radix keeps the last picked item's text in the trigger when the value returns to "": remount on unset so the placeholder (the unset row's name) shows.
    <Select key={value === "" ? "unset" : "set"} value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={label}
        title={title ?? current?.help}
        data-testid={testId}
        className={variant === "pill" ? pillTriggerClass(showLabel) : "w-full"}
      >
        {variant === "pill" && <Icons.thinking aria-hidden className="shrink-0" />}
        <SelectValue placeholder={placeholder ?? levels.find((t) => t.id === "")?.label ?? label} />
      </SelectTrigger>
      <SelectContent>
        {levels.map((t) => (
          <SelectItem key={t.id} value={t.id} title={t.help}>
            {t.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
