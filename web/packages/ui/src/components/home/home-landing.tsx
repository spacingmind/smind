import { useEffect, useRef, useState, type FormEvent } from "react";

import { AttachMenu } from "@/components/composer/attach-menu";
import { AttachmentTray } from "@/components/composer/attachment-tray";
import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { RunConfigToolbar, type RunConfigState } from "@/components/composer/run-config-toolbar";
import { useHomeAttachmentDraft } from "@/components/composer/use-attachment-draft";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { HomeIllustration } from "@/components/home/home-illustration";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { COMPOSER_ACTION_BUTTON_CLASS, COMPOSER_CARD_CLASS } from "@/lib/composer-shell";
import { useGreeting } from "@/hooks/use-greeting";
import { useProfileList } from "@/hooks/use-profiles";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProviderHealth } from "@/hooks/use-provider-health";
import { Icons } from "@/lib/icons";
import { isDesktop } from "@/lib/platform";
import { OFFICE_SUGGESTED_PROMPTS } from "@/lib/suggested-prompts";
import type { Chat, Task, Workspace } from "@/lib/types";
import { cn } from "@/lib/utils";
import type { WsClientLike } from "@/lib/ws-client";
import { attachmentDraftKey, HOME_ATTACHMENT_KEY, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useHomeProjectStore } from "@/stores/home-project-store";
import { useHomeRunConfigStore } from "@/stores/home-run-config-store";
import { useInterfaceModeStore } from "@/stores/interface-mode-store";
import { usePendingSendStore } from "@/stores/pending-send-store";

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
  connectionError,
  onRetryConnect,
  workspaces,
  onCreated,
  onAddProject,
}: {
  client: WsClientLike | null;
  connected: boolean;
  /** The initial connect's failure cause (App.tsx) -- shown above the composer while the daemon is unreachable. */
  connectionError?: string | null;
  /** Re-runs the daemon connection (App.tsx) -- the notice's Retry. */
  onRetryConnect?: () => void;
  workspaces: Workspace[];
  onCreated: (task: Task) => void;
  onAddProject?: () => void;
}) {
  const greeting = useGreeting();
  const officeMode = useInterfaceModeStore((s) => s.value) === "office";
  const storedId = useHomeProjectStore((s) => s.value);
  const setStoredId = useHomeProjectStore((s) => s.set);
  const chosen = workspaces.find((w) => w.ID === storedId) ?? workspaces[0] ?? null;
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [query, setQuery] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const attach = useHomeAttachmentDraft();

  const { providersOrFallback: providers, settled: providersSettled } = useProviderCatalog();
  const profiles = useProfileList();
  const providerHealth = useProviderHealth();
  const lastRunConfig = useHomeRunConfigStore((s) => s.value);
  const configRef = useRef<RunConfigState | null>(null);
  const sendingRef = useRef(false);

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
    if (disabled || busy || sendingRef.current || prompt.trim() === "" || attach.hasError) return;
    sendingRef.current = true;
    setBusy(true);
    setError("");
    try {
      const task = await client.call<Task>("task.create", { workspaceId: chosen.ID, title: titleFromPrompt(prompt) });
      // The task composer sends the draft's attachments with the draft text; they follow the task's default chat.
      if (attach.items.length > 0) {
        const chats = await client.call<Chat[]>("chat.list", { taskId: task.ID });
        const defaultChat = (chats ?? []).filter((c) => !c.ArchivedAt).sort((a, b) => a.ID - b.ID)[0];
        if (!defaultChat) throw new Error("The task was created but its chat could not be found; your files are still attached here.");
        useComposerAttachmentsStore.getState().move(HOME_ATTACHMENT_KEY, attachmentDraftKey(task.ID, defaultChat.ID));
      }
      try {
        window.localStorage.setItem(draftStorageKey(task.ID), prompt);
      } catch {
        // The draft is a convenience; the task exists either way.
      }
      // The task's composer takes this and sends through its own submit path; the landing never starts a run.
      if (configRef.current !== null) {
        usePendingSendStore.getState().set(task.ID, configRef.current);
        useHomeRunConfigStore.getState().set(configRef.current);
      }
      setPrompt("");
      onCreated(task);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      sendingRef.current = false;
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
        {!connected && connectionError && (
          <div
            role="alert"
            data-testid="home-connection-notice"
            className="mb-3 flex flex-col gap-2 rounded-xl border border-border bg-card px-4 py-3 text-ui-sm text-foreground"
          >
            <div className="flex flex-wrap items-center gap-2">
              <Icons.warning aria-hidden className="size-4 shrink-0 text-destructive" />
              <span>Can't reach the daemon: {connectionError}</span>
              {onRetryConnect && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="ml-auto"
                  aria-label="Retry connection"
                  onClick={onRetryConnect}
                >
                  Retry
                </Button>
              )}
            </div>
            {!isDesktop && <p className="text-ui-sm text-foreground-muted">Start it with smind serve</p>}
          </div>
        )}
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
            {attach.limits && <AttachmentTray items={attach.items} onRemove={attach.remove} />}
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
            {/* Below sm the chips take their own full-width row above attach and send (order-first); from sm up it is one row. */}
            <div data-testid="home-toolbar-row" className="flex flex-wrap items-center gap-x-1 px-2 pb-2 pt-1">
              {attach.limits && (
                <>
                  <input
                    ref={fileInput}
                    type="file"
                    multiple
                    hidden
                    tabIndex={-1}
                    aria-hidden
                    data-testid="home-attach-input"
                    onChange={(e) => {
                      attach.add(Array.from(e.target.files ?? []));
                      // Picking the same file twice must fire change again.
                      e.target.value = "";
                    }}
                  />
                  <AttachMenu disabled={disabled || busy} onPickFiles={() => fileInput.current?.click()} />
                </>
              )}
              <RunConfigToolbar
                taskId={null}
                chatId={null}
                profiles={profiles}
                providers={providers}
                providersLoading={!!client && !providersSettled}
                disabled={disabled || busy}
                providerHealth={providerHealth.health}
                onPickerOpen={providerHealth.ensure}
                initialState={lastRunConfig}
                onChange={(v) => {
                  configRef.current = v.state;
                }}
              >
                <div data-testid="home-run-chips" className="flex flex-wrap items-center max-sm:order-first max-sm:w-full">
                  <RunConfigToolbar.ModelPicker showLabel />
                  <RunConfigToolbar.Thinking showLabel />
                  <RunConfigToolbar.Mode showLabel />
                </div>
              </RunConfigToolbar>
              <Button
                type="submit"
                variant="execute"
                className={cn(COMPOSER_ACTION_BUTTON_CLASS, "ml-auto")}
                aria-label="Start task"
                title="Start task (Enter)"
                data-testid="home-send-button"
                disabled={disabled || busy || prompt.trim() === "" || attach.hasError}
              >
                <Icons.send aria-hidden />
              </Button>
            </div>
          </div>
        </form>
        {officeMode && (
          <div data-testid="home-suggestions" className="mt-4 flex flex-wrap justify-center gap-2">
            {OFFICE_SUGGESTED_PROMPTS.map((text) => (
              <Button
                key={text}
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled}
                data-testid="home-suggestion"
                onClick={() => {
                  // Fills the composer only; sending stays the person's own action.
                  setPrompt(text);
                  textareaRef.current?.focus();
                }}
              >
                {text}
              </Button>
            ))}
          </div>
        )}
        {error && (
          <p role="alert" className="mt-2 text-ui-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    </div>
  );
}
