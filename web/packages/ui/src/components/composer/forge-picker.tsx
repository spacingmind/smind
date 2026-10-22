import { useEffect, useId, useState, type KeyboardEvent } from "react";

import type { ForgeKind } from "@/components/composer/attach-menu";
import { Input } from "@/components/ui/input";
import { useForgeSearch, type ForgeSearchItem } from "@/hooks/use-forge-search";
import { attachmentErrorMessage } from "@/lib/attachments/errors";
import { Icons } from "@/lib/icons";
import { formatRelativeTime } from "@/lib/relative-time";
import type { AttachmentLimits } from "@/lib/types";
import { cn } from "@/lib/utils";

const DEBOUNCE_MS = 300;

/**
 * The GitHub pull request / issue picker (ADR-0038 §4): a search field over `forge.search`. A number or a github.com
 * URL typed into the field resolves on the daemon, so nothing is parsed here. Picking hands back the item and the
 * repo the search ran against.
 */
export function ForgePicker({
  kind,
  taskId,
  limits,
  onPick,
}: {
  kind: ForgeKind;
  taskId: number;
  limits: AttachmentLimits | null;
  onPick: (item: ForgeSearchItem, repo: string) => void;
}) {
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();

  // Search once per settled query, not per keystroke.
  useEffect(() => {
    if (text === query) return;
    const timer = setTimeout(() => setQuery(text), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text, query]);

  const search = useForgeSearch({ taskId, kind, query, enabled: true });
  const items = search.data?.items ?? [];
  const activeIndex = Math.min(active, Math.max(items.length - 1, 0));
  const noun = kind === "pr" ? "pull requests" : "issues";

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (items.length === 0) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((activeIndex + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      onPick(items[activeIndex]!, search.data!.repo);
    }
  }

  return (
    <div data-testid="forge-picker" className="flex flex-col">
      <div className="border-b border-border p-2">
        <Input
          autoFocus
          role="combobox"
          aria-label={kind === "pr" ? "Search pull requests" : "Search issues"}
          aria-expanded
          aria-controls={listId}
          aria-activedescendant={items.length > 0 ? `${listId}-${activeIndex}` : undefined}
          placeholder={kind === "pr" ? "Search pull requests, or paste a number or URL" : "Search issues, or paste a number or URL"}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setActive(0);
          }}
          onKeyDown={handleKeyDown}
        />
      </div>
      <div id={listId} role="listbox" aria-label={kind === "pr" ? "Pull requests" : "Issues"} className="max-h-72 overflow-auto p-1">
        {search.isError ? (
          <p role="alert" className="flex items-start gap-2 px-2 py-1.5 text-ui-sm text-destructive">
            <Icons.warning aria-hidden className="mt-0.5 size-4 shrink-0" />
            {attachmentErrorMessage(search.error, limits)}
          </p>
        ) : search.isPending ? (
          <p role="status" className="flex items-center gap-2 px-2 py-1.5 text-ui-sm text-foreground-muted">
            <Icons.spinner aria-hidden className="size-4 animate-spin" />
            Searching…
          </p>
        ) : items.length === 0 ? (
          <p role="status" className="px-2 py-1.5 text-ui-sm text-foreground-muted">
            {query ? `No matching ${noun}` : `No open ${noun}`}
          </p>
        ) : (
          items.map((item, index) => {
            const Icon = kind === "pr" ? Icons.pullRequest : Icons.issue;
            return (
              <div
                key={item.number}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={index === activeIndex}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => onPick(item, search.data!.repo)}
                onMouseMove={() => index !== activeIndex && setActive(index)}
                className={cn("flex cursor-pointer items-start gap-2 rounded-md px-2 py-1.5", index === activeIndex && "bg-menu-hover")}
              >
                <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-foreground-muted" />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-ui-sm text-foreground">
                    #{item.number} {item.title}
                  </span>
                  <span className="truncate text-ui-xs text-foreground-muted">
                    {item.author} · {item.state} · {formatRelativeTime(item.updatedAt)}
                  </span>
                </span>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
