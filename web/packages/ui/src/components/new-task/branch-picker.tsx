import { useEffect, useId, useState, type KeyboardEvent } from "react";

import { Input } from "@/components/ui/input";
import { useWorkspaceBranches } from "@/hooks/use-workspace-branches";
import { Icons } from "@/lib/icons";
import type { WorkspaceBranch } from "@/lib/types";
import { cn } from "@/lib/utils";

const DEBOUNCE_MS = 300;

/** `+N −M` against the default base; an unknown count (omitted by the daemon) shows no token, never 0. */
export function divergence(b: WorkspaceBranch): string[] {
  return [b.ahead !== undefined ? `+${b.ahead}` : "", b.behind !== undefined ? `−${b.behind}` : ""].filter(Boolean);
}

/** A combobox over `workspace.branches` (local and origin, newest first), searched on the daemon with a debounced query. */
export function BranchPicker({ workspaceId, onPick }: { workspaceId: number; onPick: (branch: WorkspaceBranch) => void }) {
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();

  useEffect(() => {
    if (text === query) return;
    const timer = setTimeout(() => setQuery(text), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text, query]);

  const branches = useWorkspaceBranches({ workspaceId, query });
  const items = branches.data?.branches ?? [];
  const activeIndex = Math.min(active, Math.max(items.length - 1, 0));

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    // Enter inside the dialog's form must pick, never submit.
    if (e.key === "Enter") e.preventDefault();
    if (items.length === 0) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((activeIndex + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length);
    } else if (e.key === "Enter") {
      onPick(items[activeIndex]!);
    }
  }

  return (
    <div data-testid="branch-picker" className="flex flex-col rounded-lg border border-input-border">
      <div className="border-b border-border p-2">
        <Input
          role="combobox"
          aria-label="Search branches"
          aria-expanded
          aria-controls={listId}
          aria-activedescendant={items.length > 0 ? `${listId}-${activeIndex}` : undefined}
          placeholder="Search branches"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setActive(0);
          }}
          onKeyDown={handleKeyDown}
        />
      </div>
      <div id={listId} role="listbox" aria-label="Branches" className="max-h-56 overflow-auto p-1">
        {branches.isError ? (
          <p role="alert" className="flex items-start gap-2 px-2 py-1.5 text-ui-sm text-destructive">
            <Icons.warning aria-hidden className="mt-0.5 size-4 shrink-0" />
            {branches.error.message}
          </p>
        ) : branches.isPending ? (
          <p role="status" className="flex items-center gap-2 px-2 py-1.5 text-ui-sm text-foreground-muted">
            <Icons.spinner aria-hidden className="size-4 animate-spin" />
            Loading branches…
          </p>
        ) : items.length === 0 ? (
          <p role="status" className="px-2 py-1.5 text-ui-sm text-foreground-muted">
            {query ? "No matching branches" : "No branches"}
          </p>
        ) : (
          items.map((b, index) => (
            <div
              key={`${b.remote}:${b.name}`}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={index === activeIndex}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onPick(b)}
              onMouseMove={() => index !== activeIndex && setActive(index)}
              className={cn("flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5", index === activeIndex && "bg-menu-hover")}
            >
              <Icons.branch aria-hidden className="size-4 shrink-0 text-foreground-muted" />
              <span className="min-w-0 flex-1 truncate font-mono text-ui-sm text-foreground">{b.name}</span>
              {divergence(b).map((token) => (
                <span key={token} className="shrink-0 font-mono text-ui-xs text-foreground-muted">
                  {token}
                </span>
              ))}
              <span className="shrink-0 rounded border border-border px-1 text-ui-xs text-foreground-muted">{b.remote === "origin" ? "origin" : "local"}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
