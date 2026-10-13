import { useState } from "react";
import { Icons } from "@/lib/icons";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

const isString = (value: unknown): value is string => typeof value === "string";

/**
 * ZCode's "open in editor" pill (WorkspaceEditorButtonGroup): a split button
 * in the header. The main half opens the task's worktree in the remembered
 * editor; the chevron lists every installed editor (picking one opens it and
 * becomes the new default) plus the file manager. Editors come from the
 * Rust-side allowlist (D4), so this only exists where `useLocalPathActions`
 * does: a desktop build on the local connection. With no editor installed
 * there is nothing to pick, so the pill is absent (the ⋯ menu still reveals).
 */
export function OpenInEditorButton({ path, actions }: { path: string; actions: LocalPathActions }) {
  const [preferredId, setPreferredId] = useState(() => readStored(STORAGE_KEYS.preferredEditor, "", isString));
  const { tooltip, onMenuOpenChange } = useMenuTooltip();
  const { editors } = actions;
  const active = editors.find((e) => e.id === preferredId) ?? editors[0];
  if (!active) return null;

  const choose = (id: string) => {
    setPreferredId(id);
    writeStored(STORAGE_KEYS.preferredEditor, id);
    actions.open(id, path);
  };

  return (
    <div
      data-testid="open-in-editor"
      className="inline-flex h-7 items-stretch overflow-hidden rounded-lg border border-border text-foreground-muted dark:border-input-border"
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-testid="open-in-editor-main"
            aria-label={`Open in ${active.label}`}
            className="h-full rounded-none px-2 text-foreground-muted hover:text-foreground"
            onClick={() => actions.open(active.id, path)}
          >
            <Icons.openInEditor aria-hidden />
            <span className="hidden @min-[56rem]/app-header:inline">{active.label}</span>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">Open in {active.label}</TooltipContent>
      </Tooltip>
      <DropdownMenu onOpenChange={onMenuOpenChange}>
        <Tooltip {...tooltip}>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                data-testid="open-in-editor-picker"
                aria-label="Choose editor"
                className="h-full w-5 rounded-none border-l border-border text-foreground-muted hover:text-foreground dark:border-input-border"
              >
                <Icons.chevronDown aria-hidden />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="bottom">Choose editor</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" data-testid="open-in-editor-menu">
          {editors.map((editor) => (
            <DropdownMenuItem
              key={editor.id}
              data-testid="open-in-editor-item"
              data-editor-id={editor.id}
              onSelect={() => choose(editor.id)}
            >
              <Icons.openInEditor aria-hidden />
              <span className="min-w-0 flex-1 truncate">{editor.label}</span>
              {editor.id === active.id && <Icons.check aria-hidden className="ml-2 size-4" />}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem data-testid="open-in-editor-reveal" onSelect={() => actions.reveal(path)}>
            <Icons.folderOpen aria-hidden />
            {actions.revealLabel}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
