import { useEffect, useRef, useState, type FormEvent } from "react";

import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { HomeIllustration } from "@/components/home/home-illustration";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { COMPOSER_ACTION_BUTTON_CLASS, COMPOSER_CARD_CLASS } from "@/lib/composer-shell";
import { useGreeting } from "@/hooks/use-greeting";
import { Icons } from "@/lib/icons";
import type { Task, Workspace } from "@/lib/types";
import { cn } from "@/lib/utils";
import type { WsClientLike } from "@/lib/ws-client";
import { useHomeProjectStore } from "@/stores/home-project-store";

/** Workspaces needed before the project menu shows its search box. */
const SEARCH_THRESHOLD = 6;
const MAX_TITLE_LENGTH = 60;

function workspaceName(w: Workspace): string {
  return w.Title || w.Path.split("/").filter(Boolean).pop() || w.Path;
}

/** The first non-empty line, whitespace collapsed and cut to MAX_TITLE_LENGTH with an ellipsis. */
function titleFromPrompt(prompt: string): string {
  const line = prompt.split("\n").find((l) => l.trim() !== "") ?? "";
  const title = line.trim().replace(/\s+/g, " ");
  return title.length > MAX_TITLE_LENGTH ? `${title.slice(0, MAX_TITLE_LENGTH - 1)}…` : title;
}

function placeholderFor(connected: boolean, hasProjects: boolean): string {
  if (!connected) return "Not connected — reconnecting to the daemon…";
  if (!hasProjects) return "Add a project to start a task";
  return "Describe a task for the agent…";
}

/** The no-task-selected landing: pick a project, describe a task, and the created task opens with the prompt as its draft. */
export function HomeLanding({
  client,
  connected,
  workspaces,
  onCreated,
  onAddProject,
}: {
  client: WsClientLike | null;
  connected: boolean;
  workspaces: Workspace[];
  onCreated: (task: Task) => void;
  onAddProject?: () => void;
}) {
  const greeting = useGreeting();
  const storedId = useHomeProjectStore((s) => s.value);
  const setStoredId = useHomeProjectStore((s) => s.set);
  const chosen = workspaces.find((w) => w.ID === storedId) ?? workspaces[0] ?? null;
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [query, setQuery] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const disabled = !connected || chosen === null || client === null;

  useEffect(() => {
    if (!disabled) textareaRef.current?.focus();
    // Focus once on mount only; later enabling must not steal focus from the project menu.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const needle = query.trim().toLowerCase();
  const visible = needle
    ? workspaces.filter((w) => w.Title.toLowerCase().includes(needle) || w.Path.toLowerCase().includes(needle))
    : workspaces;

  async function submit() {
    if (disabled || busy || prompt.trim() === "") return;
    setBusy(true);
    setError("");
    try {
      const task = await client.call<Task>("task.create", { workspaceId: chosen.ID, title: titleFromPrompt(prompt) });
      try {
        window.localStorage.setItem(draftStorageKey(task.ID), prompt);
      } catch {
        // The draft is a convenience; the task exists either way.
      }
      setPrompt("");
      onCreated(task);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    void submit();
  }

  return (
    <div data-testid="home-landing" className="flex h-full flex-col items-center justify-center gap-6 overflow-y-auto bg-panel px-4 py-10">
      <HomeIllustration />
      <h1 className="text-ui-xl font-semibold text-foreground">{greeting}</h1>
      <div className="w-full max-w-2xl">
        <div className="-mb-3 rounded-t-2xl bg-muted/60 px-1.5 pb-4 pt-1.5">
          <Popover
            open={menuOpen}
            onOpenChange={(open) => {
              setMenuOpen(open);
              if (!open) setQuery("");
            }}
          >
            <PopoverTrigger asChild>
              <Button type="button" variant="ghost" size="sm" aria-label="Project" data-testid="home-project-chip">
                <Icons.folder aria-hidden />
                <span className="max-w-[16rem] truncate">{chosen ? workspaceName(chosen) : "No project"}</span>
                <Icons.chevronDown aria-hidden />
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-[260px] rounded-xl bg-menu p-1">
              {workspaces.length >= SEARCH_THRESHOLD && (
                <div className="relative p-1">
                  <Icons.search aria-hidden className="pointer-events-none absolute left-3.5 top-1/2 size-3.5 -translate-y-1/2 text-foreground-muted" />
                  <Input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search projects"
                    aria-label="Search projects"
                    data-testid="home-project-search"
                    className="pl-8"
                  />
                </div>
              )}
              <div role="listbox" aria-label="Projects" className="max-h-64 overflow-y-auto">
                {visible.map((w) => (
                  <button
                    key={w.ID}
                    type="button"
                    role="option"
                    aria-selected={w.ID === chosen?.ID}
                    data-testid={`home-project-option-${w.ID}`}
                    onClick={() => {
                      setStoredId(w.ID);
                      setMenuOpen(false);
                    }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-ui-base hover:bg-menu-hover"
                  >
                    <Icons.folder aria-hidden className="size-4 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{workspaceName(w)}</span>
                      <span className="block truncate text-ui-sm text-foreground-muted">{w.Path}</span>
                    </span>
                    {w.ID === chosen?.ID && <Icons.check aria-hidden className="size-4 shrink-0" />}
                  </button>
                ))}
                {visible.length === 0 && <p className="px-2 py-1.5 text-ui-sm text-foreground-muted">No projects</p>}
              </div>
              {onAddProject && (
                <>
                  <Separator className="my-1" />
                  <button
                    type="button"
                    data-testid="home-project-add"
                    onClick={() => {
                      setMenuOpen(false);
                      onAddProject();
                    }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-ui-base hover:bg-menu-hover"
                  >
                    <Icons.add aria-hidden className="size-4 shrink-0" />
                    Add project
                  </button>
                </>
              )}
            </PopoverContent>
          </Popover>
        </div>
        <form data-testid="home-composer" onSubmit={onSubmit} className="relative">
          <div data-testid="home-composer-card" className={cn(COMPOSER_CARD_CLASS)}>
            <PromptTextarea
              ref={textareaRef}
              label="Prompt"
              rows={3}
              value={prompt}
              onChange={setPrompt}
              onSubmit={() => void submit()}
              placeholder={placeholderFor(connected, workspaces.length > 0)}
              disabled={disabled}
              className="rounded-none border-0 bg-transparent px-4 pb-1 pt-3.5 focus-visible:border-transparent focus-visible:ring-0"
            />
            <div className="flex items-center justify-end px-2 pb-2 pt-1">
              <Button
                type="submit"
                variant="execute"
                className={COMPOSER_ACTION_BUTTON_CLASS}
                aria-label="Start task"
                title="Start task (Enter)"
                data-testid="home-send-button"
                disabled={disabled || busy || prompt.trim() === ""}
              >
                <Icons.send aria-hidden />
              </Button>
            </div>
          </div>
        </form>
        {error && (
          <p role="alert" className="mt-2 text-ui-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    </div>
  );
}
