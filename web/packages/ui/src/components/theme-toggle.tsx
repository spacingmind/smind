
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/hooks/use-theme";
import type { ThemePreference } from "@/lib/theme";
import { Icons } from "@/lib/icons";

const OPTIONS: { id: ThemePreference; label: string; icon: typeof Icons.themeLight }[] = [
  { id: "light", label: "Light", icon: Icons.themeLight },
  { id: "dark", label: "Dark", icon: Icons.themeDark },
  { id: "system", label: "System", icon: Icons.themeSystem },
];

/**
 * The theme control (ui-redesign-parity plan, Item 1's "reachable
 * somewhere" requirement) -- mounted in the sidebar header today, next to
 * the notifications/accounts icons; Item 13's settings screen is the
 * eventual long-term home. The trigger icon reflects what's actually
 * applied (`resolved`), not the raw preference, so "System" while the OS
 * is dark still shows the moon.
 */
export function ThemeToggle({ withTooltip = false, className }: { withTooltip?: boolean; className?: string }) {
  const { preference, resolved, setPreference } = useTheme();
  const themeTip = useMenuTooltip();
  const TriggerIcon = resolved === "dark" ? Icons.themeDark : Icons.themeLight;

  const trigger = (
    <Button variant="ghost" size="icon-sm" aria-label={`Theme: ${preference}`} data-testid="theme-toggle-trigger" className={className}>
      <TriggerIcon />
    </Button>
  );

  return (
    <DropdownMenu onOpenChange={themeTip.onMenuOpenChange}>
      {withTooltip ? (
        // Needs a TooltipProvider above (the sidebar footer supplies one).
        <Tooltip {...themeTip.tooltip}>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="top">Theme</TooltipContent>
        </Tooltip>
      ) : (
        <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      )}
      <DropdownMenuContent align="end" side={withTooltip ? "top" : undefined}>
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
              {active && <Icons.check className="ml-auto size-4" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
