import { Icons } from "@/lib/icons";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
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
}) {
  const current = levels.find((t) => t.id === value);
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={label}
        title={title ?? current?.help}
        data-testid={testId}
        className={variant === "pill" ? PILL_TRIGGER_CLASS : "w-full"}
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
