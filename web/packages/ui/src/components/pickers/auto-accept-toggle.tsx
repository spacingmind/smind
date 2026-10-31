import { Checkbox } from "@/components/ui/checkbox";
import { AUTO_ACCEPT_HELP, AUTO_ACCEPT_LABEL } from "@/lib/permission-modes";
import { Icons } from "@/lib/icons";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** The shared Auto-accept control: pill variant is the composer toolbar's ghost toggle; field variant is the profile form's checkbox label. */
export function AutoAcceptToggle({
  checked,
  onChange,
  disabled = false,
  pending = false,
  variant,
  testId,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  pending?: boolean;
  variant: "pill" | "field";
  testId?: string;
}) {
  if (variant === "field") {
    return (
      <label className="flex items-center gap-2 text-ui-sm text-foreground-muted" title={AUTO_ACCEPT_HELP}>
        <Checkbox
          data-testid={testId}
          checked={checked}
          disabled={disabled}
          onCheckedChange={(on) => onChange(on === true)}
        />
        {AUTO_ACCEPT_LABEL} every permission prompt
      </label>
    );
  }
  return (
    <Button
      type="button"
      variant="ghost"
      aria-pressed={checked}
      aria-busy={pending || undefined}
      title={AUTO_ACCEPT_HELP}
      data-testid={testId ?? "composer-auto-accept"}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      aria-label="Auto-accept"
      className={cn(PILL_TRIGGER_CLASS, checked ? "text-warning hover:text-warning" : "text-foreground-muted")}
    >
      <Icons.checkAll aria-hidden className="shrink-0" />
      <span className="max-sm:hidden">{AUTO_ACCEPT_LABEL}</span>
    </Button>
  );
}
