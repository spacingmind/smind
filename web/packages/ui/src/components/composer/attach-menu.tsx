import { useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Icons } from "@/lib/icons";
import { MENU_CLOSE_INSTANT } from "@/lib/menu-item-disabled";

export type ForgeKind = "pr" | "issue";

/** The GitHub entries of the menu: which kinds the daemon lists and, when `gh` is missing, why they are disabled. */
export interface AttachMenuForge {
  kinds: ForgeKind[];
  disabledReason: string | null;
  onOpen: (kind: ForgeKind) => void;
}

function Entry({ icon, label, description }: { icon: ReactNode; label: string; description?: string }) {
  return (
    <>
      {icon}
      <span className="flex min-w-0 flex-col">
        <span>{label}</span>
        {description && <span className="text-ui-xs text-foreground-muted">{description}</span>}
      </span>
    </>
  );
}

/**
 * The composer's Attach button (ADR-0038 §6): an icon button that opens a menu of what can be attached. Entries the
 * daemon cannot take are absent, not dead; the GitHub ones are shown disabled (with the reason) only when `gh` is missing.
 */
export function AttachMenu({
  disabled,
  onPickFiles,
  onMention,
  forge,
  onCloseAutoFocus,
}: {
  disabled: boolean;
  onPickFiles: () => void;
  /** Present when the daemon accepts `file_ref`. */
  onMention?: () => void;
  forge?: AttachMenuForge;
  /** Radix returns focus to the trigger on close; the composer redirects it to the textarea after a mention. */
  onCloseAutoFocus?: (e: Event) => void;
}) {
  // Choosing an entry closes the menu on the very frame the thing it opens appears (the picker, the mention list), so
  // the menu never fades out underneath it; Esc or a click outside keep the normal fade. Reset when the menu reopens.
  const [instantClose, setInstantClose] = useState(false);
  const choose = (action: () => void) => () => {
    setInstantClose(true);
    action();
  };
  return (
    <DropdownMenu onOpenChange={(open) => open && setInstantClose(false)}>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" className="size-8 rounded-lg" disabled={disabled} aria-label="Attach" title="Attach" data-testid="composer-attach-button">
          <Icons.attach aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className={instantClose ? `min-w-56 ${MENU_CLOSE_INSTANT}` : "min-w-56"} onCloseAutoFocus={onCloseAutoFocus}>
        <DropdownMenuItem data-testid="attach-menu-files" onSelect={choose(onPickFiles)}>
          <Entry icon={<Icons.attach aria-hidden />} label="Image or file…" />
        </DropdownMenuItem>
        {onMention && (
          <DropdownMenuItem data-testid="attach-menu-mention" onSelect={choose(onMention)}>
            <Entry icon={<Icons.mention aria-hidden />} label="Mention a file" />
          </DropdownMenuItem>
        )}
        {forge?.kinds.includes("pr") && (
          <DropdownMenuItem data-testid="attach-menu-pr" disabled={forge.disabledReason !== null} onSelect={choose(() => forge.onOpen("pr"))}>
            <Entry icon={<Icons.pullRequest aria-hidden />} label="GitHub pull request…" description={forge.disabledReason ?? undefined} />
          </DropdownMenuItem>
        )}
        {forge?.kinds.includes("issue") && (
          <DropdownMenuItem data-testid="attach-menu-issue" disabled={forge.disabledReason !== null} onSelect={choose(() => forge.onOpen("issue"))}>
            <Entry icon={<Icons.issue aria-hidden />} label="GitHub issue…" description={forge.disabledReason ?? undefined} />
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
