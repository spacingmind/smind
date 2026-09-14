import { Check, Monitor, Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/hooks/use-theme";
import type { ThemePreference } from "@/lib/theme";

const OPTIONS: { id: ThemePreference; label: string; icon: typeof Sun }[] = [
  { id: "light", label: "Light", icon: Sun },
  { id: "dark", label: "Dark", icon: Moon },
  { id: "system", label: "System", icon: Monitor },
];

/**
 * The theme control (ui-redesign-parity plan, Item 1's "reachable
 * somewhere" requirement) -- mounted in the sidebar header today, next to
 * the notifications/accounts icons; Item 13's settings screen is the
 * eventual long-term home. The trigger icon reflects what's actually
 * applied (`resolved`), not the raw preference, so "System" while the OS
 * is dark still shows the moon.
 */
export function ThemeToggle() {
  const { preference, resolved, setPreference } = useTheme();
  const TriggerIcon = resolved === "dark" ? Moon : Sun;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`Theme: ${preference}`} data-testid="theme-toggle-trigger">
          <TriggerIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {OPTIONS.map((option) => {
          const Icon = option.icon;
          const active = preference === option.id;
          return (
            <DropdownMenuItem
              key={option.id}
              data-testid={`theme-option-${option.id}`}
              aria-checked={active}
              role="menuitemradio"
              onClick={() => setPreference(option.id)}
            >
              <Icon />
              {option.label}
              {active && <Check className="ml-auto size-3.5" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
