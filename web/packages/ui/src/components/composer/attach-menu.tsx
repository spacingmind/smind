import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Icons } from "@/lib/icons";

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
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" className="size-8 rounded-lg" disabled={disabled} aria-label="Attach" title="Attach" data-testid="composer-attach-button">
          <Icons.attach aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="min-w-56" onCloseAutoFocus={onCloseAutoFocus}>
        <DropdownMenuItem data-testid="attach-menu-files" onSelect={onPickFiles}>
          <Entry icon={<Icons.attach aria-hidden />} label="Image or file…" />
        </DropdownMenuItem>
        {onMention && (
          <DropdownMenuItem data-testid="attach-menu-mention" onSelect={onMention}>
            <Entry icon={<Icons.mention aria-hidden />} label="Mention a file" />
          </DropdownMenuItem>
        )}
        {forge?.kinds.includes("pr") && (
          <DropdownMenuItem data-testid="attach-menu-pr" disabled={forge.disabledReason !== null} onSelect={() => forge.onOpen("pr")}>
            <Entry icon={<Icons.pullRequest aria-hidden />} label="GitHub pull request…" description={forge.disabledReason ?? undefined} />
          </DropdownMenuItem>
        )}
        {forge?.kinds.includes("issue") && (
          <DropdownMenuItem data-testid="attach-menu-issue" disabled={forge.disabledReason !== null} onSelect={() => forge.onOpen("issue")}>
            <Entry icon={<Icons.issue aria-hidden />} label="GitHub issue…" description={forge.disabledReason ?? undefined} />
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
