import type { SlashRpcStatus } from "@/components/composer/use-slash-command";
import type { SlashCommand } from "@/lib/slash-commands";
import { cn } from "@/lib/utils";

export const SLASH_LISTBOX_ID = "composer-slash-listbox";
export const slashOptionId = (index: number) => `composer-slash-option-${index}`;

/**
 * The `/` command list: a listbox the composer keeps focus-neutral (the textarea stays the focused element and
 * points at the active row with aria-activedescendant), shown and hidden instantly like the `@` list. A plugin's
 * command carries a quiet "from <plugin>" suffix.
 */
export function SlashCommandPopover({
  rows,
  activeIndex,
  onPick,
  onHover,
  status = null,
}: {
  rows: SlashCommand[];
  activeIndex: number;
  onPick: (command: SlashCommand) => void;
  onHover: (index: number) => void;
  /** The last RPC-backed command: a "Running…" mark on its row, or its typed failure under the list. */
  status?: SlashRpcStatus | null;
}) {
  return (
    <div
      id={SLASH_LISTBOX_ID}
      role="listbox"
      aria-label="Commands"
      data-testid="slash-command-popover"
      className="absolute inset-x-0 bottom-full z-20 mb-1 max-h-60 overflow-auto rounded-lg border border-border bg-menu shadow-md"
    >
      {rows.map((row, index) => (
        <div
          key={row.key}
          id={slashOptionId(index)}
          role="option"
          aria-selected={index === activeIndex}
          // Keep the textarea focused: a click picks, it never moves focus.
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onPick(row)}
          onMouseMove={() => index !== activeIndex && onHover(index)}
          className={cn("flex cursor-pointer items-baseline gap-2 px-3 py-1.5 text-ui-sm", index === activeIndex && "bg-menu-hover")}
        >
          <span className="shrink-0 font-medium text-foreground">/{row.label}</span>
          {row.description && <span className="min-w-0 truncate text-foreground-muted">{row.description}</span>}
          {status?.state === "running" && status.key === row.key ? (
            <span className="ml-auto shrink-0 pl-2 text-ui-xs text-foreground-subtle" role="status">
              Running…
            </span>
          ) : (
            !row.builtin && <span className="ml-auto shrink-0 pl-2 text-ui-xs text-foreground-subtle">from {row.source}</span>
          )}
        </div>
      ))}
      {status?.state === "error" && (
        <div role="alert" data-testid="slash-command-error" data-code={status.code} className="border-t border-border px-3 py-1.5 text-ui-xs text-destructive">
          {status.message}
        </div>
      )}
    </div>
  );
}
