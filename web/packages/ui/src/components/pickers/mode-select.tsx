import { Icons } from "@/lib/icons";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { ModeInfo } from "@/lib/types";

/**
 * The shared permission-mode Select (ADR-0019): pill variant is the
 * composer toolbar's label-less trigger; field variant is a full-width
 * form control (the Settings profile form's shape), optionally with
 * leading extra items such as "Provider default".
 */
export function ModeSelect({
  modes,
  value,
  onChange,
  disabled = false,
  dangerous = false,
  variant,
  testId,
  extraItems,
}: {
  modes: ModeInfo[];
  value: string;
  onChange: (modeId: string) => void;
  disabled?: boolean;
  /** The mode auto-approves (or ACP Auto-accept is on) -- warn without changing semantics. */
  dangerous?: boolean;
  variant: "pill" | "field";
  testId?: string;
  /** Rendered before the provider's own modes (field variant's "Provider default" sentinel). */
  extraItems?: ModeInfo[];
}) {
  const current = modes.find((m) => m.id === value);
  const items = [...(extraItems ?? []), ...modes];
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label="Permission mode"
        title={current?.description}
        data-testid={testId}
        data-danger={dangerous ? "true" : undefined}
        className={variant === "pill" ? cn(PILL_TRIGGER_CLASS, dangerous && "text-warning hover:text-warning") : "w-full"}
      >
        {variant === "pill" &&
          (dangerous ? <Icons.permissionDanger aria-hidden className="shrink-0" /> : <Icons.permission aria-hidden className="shrink-0" />)}
        <SelectValue placeholder="Permission mode" />
      </SelectTrigger>
      <SelectContent>
        {items.map((m) => (
          <SelectItem key={m.id} value={m.id} title={m.description} className={m.autoApproves ? "text-warning" : undefined}>
            {m.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
