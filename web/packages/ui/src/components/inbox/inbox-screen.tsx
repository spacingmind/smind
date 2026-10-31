import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";

import { AutomationChip } from "@/components/automations/automation-chip";
import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { HostChip } from "@/components/host-chip";
import { PermissionCard } from "@/components/permission/permission-card";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { useFleetHosts } from "@/hooks/use-fleet-hosts";
import { useInboxActions, useInboxCount, useInboxList, useInboxRequest, useRespondPermission } from "@/hooks/use-inbox";
import { useIsMobile } from "@/hooks/use-mobile";
import { useWindowState } from "@/hooks/use-window-state";
import { Icons } from "@/lib/icons";
import { queryKeys } from "@/lib/query-keys";
import type { WorkspaceWithTree } from "@/lib/workspace-tree";
import { canSnoozeOrResolve, groupInbox, inboxAnnouncement, SNOOZE_OPTIONS, type InboxItem } from "@/lib/inbox-types";
import { desktopOS, isDesktop } from "@/lib/platform";
import { formatRelativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { headerLeftInsetPx } from "@/lib/window-chrome";

const BASE_PADDING_PX = 8;

/** The tone of an item's pill: a failed or blocked verdict and a permission ask for attention, a ready pass and a stall are calmer. */
function tone(item: InboxItem): StatusBadgeStatus {
  if (item.kind === "permission" || item.kind === "host_pair") return "warning";
  if (item.kind === "stall" || item.kind === "host_down") return "neutral";
  if (item.kind === "host_refused") return "danger";
  return item.attention === "high" ? "danger" : "success";
}

function kindLabel(item: InboxItem): string {
  if (item.kind === "permission") return "Permission";
  if (item.kind === "stall") return "Stalled";
  if (item.kind === "host_pair") return "Pairing";
  if (item.kind === "host_refused") return "Refused";
  if (item.kind === "host_down") return "Unreachable";
  return item.attention === "high" ? "Needs a decision" : "Passed";
}

function isHostKind(kind: InboxItem["kind"]): boolean {
  return kind === "host_pair" || kind === "host_down" || kind === "host_refused";
}

function KindIcon({ item }: { item: InboxItem }) {
  const Icon =
    item.kind === "permission"
      ? Icons.permission
      : item.kind === "stall"
        ? Icons.stalled
        : item.kind === "host_pair" || item.kind === "host_down"
          ? Icons.host
          : item.kind === "host_refused"
            ? Icons.permissionDanger
            : Icons.verdict;
  return <Icon aria-hidden className="size-4" />;
}

/**
 * The Human inbox (ADR-0045 section 5): one list of what needs a person. It is a full-screen overlay like Settings
 * (no router in this app), reached from the sidebar footer. A permission row expands to the SAME permission card the
 * task page shows (full path, diff and options) and is answered through the same `run.respondPermission`; the inbox
 * itself has no approve-all, no merge button and no "always allow". `e` (resolve) and `s` (snooze) never act on a
 * permission, which can only be answered.
 */
export function InboxScreen({
  onNavigateBack,
  onOpenTask,
  onOpenSettings,
}: {
  onNavigateBack: () => void;
  onOpenTask: (taskId: number) => void;
  /** Host rows have no task: they lead to a Settings section (the Hosts list). */
  onOpenSettings?: (sectionId: string) => void;
}) {
  const list = useInboxList();
  const automationLinks = useAutomationLinks();
  const count = useInboxCount();
  const actions = useInboxActions();
  const fleetHosts = useFleetHosts();
  const hostAlias = (hostId: string) => fleetHosts.data?.hosts.find((h) => h.hostId === hostId)?.alias;
  const narrow = useIsMobile();
  const { fullscreen } = useWindowState();
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState("");
  const listRef = useRef<HTMLDivElement>(null);

  const items = list.data ?? [];
  const groups = groupInbox(items);
  const unseen = count.data?.unseen;

  // The polite live region: a spoken line only when the count CHANGES after the first load (never a focus move).
  const lastUnseen = useRef<number | null>(null);
  useEffect(() => {
    if (unseen === undefined) return;
    if (lastUnseen.current !== null && lastUnseen.current !== unseen) setAnnouncement(inboxAnnouncement(unseen));
    lastUnseen.current = unseen;
  }, [unseen]);

  // Escape closes the screen, unless something on top (a menu, a dialog) already consumed it.
  useEffect(() => {
    function onKeyDown(e: globalThis.KeyboardEvent) {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const open = document.querySelectorAll('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]');
      if (Array.from(open).some((el) => !el.closest("[inert]"))) return;
      onNavigateBack();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onNavigateBack]);

  const run = async (fn: () => Promise<void>) => {
    setError(null);
    try {
      await fn();
    } catch (err) {
      setError(err instanceof Error ? err.message : "That did not work.");
    }
  };
  const open = (item: InboxItem) => {
    const taskId = item.links.taskId ?? item.taskId;
    if (taskId === undefined) return;
    void run(async () => {
      await actions.seen([item.key]);
    });
    onOpenTask(taskId);
  };

  const openHosts = (item: InboxItem) => {
    void run(async () => {
      await actions.seen([item.key]);
    });
    onOpenSettings?.("hosts");
  };

  // j/k/arrows move between rows; Enter, s and e act on the focused ROW itself (never on a control inside it).
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const row = (e.target as HTMLElement).closest<HTMLElement>("[data-inbox-row]");
    if (!row || e.target !== row) return;
    const rows = Array.from(listRef.current?.querySelectorAll<HTMLElement>("[data-inbox-row]") ?? []);
    const at = rows.indexOf(row);
    const item = items.find((i) => i.key === row.dataset.inboxRow);
    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      rows[Math.min(at + 1, rows.length - 1)]?.focus();
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      rows[Math.max(at - 1, 0)]?.focus();
    } else if (!item) {
      return;
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (item.kind === "permission") setExpanded((cur) => (cur === item.key ? null : item.key));
      else if (isHostKind(item.kind)) {
        if (onOpenSettings) openHosts(item);
      } else open(item);
    } else if (e.key === "s" && canSnoozeOrResolve(item)) {
      e.preventDefault();
      void run(() => actions.snooze([item], SNOOZE_OPTIONS[0].ms));
    } else if (e.key === "e" && canSnoozeOrResolve(item)) {
      // Never reached for a permission: it can only be answered.
      e.preventDefault();
      void run(() => actions.resolve([item]));
    }
  };

  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;
  const headerStyle = leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined;

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background" data-testid="inbox-screen">
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
        data-testid="inbox-header"
        data-tauri-drag-region={isDesktop ? "deep" : undefined}
        style={headerStyle}
      >
        <Button variant="ghost" size="icon" aria-label="Back to app" data-testid="inbox-back-button" onClick={onNavigateBack}>
          <Icons.back className="size-4" />
        </Button>
        <h1 className="truncate text-ui-base font-medium">Inbox</h1>
        {unseen !== undefined && unseen > 0 && (
          <StatusBadge status="neutral" data-testid="inbox-unseen">
            {unseen} unseen
          </StatusBadge>
        )}
        <div className="ml-auto" />
        <DesktopWindowControls />
      </header>
      <div role="status" aria-live="polite" className="sr-only" data-testid="inbox-live">
        {announcement}
      </div>
      <main className="min-h-0 flex-1 overflow-y-auto" data-testid="inbox-content">
        <div ref={listRef} className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-4" onKeyDown={onKeyDown}>
          {error && <FormError message={error} testId="inbox-error" />}
          {list.isPending ? (
            <LoadingRows />
          ) : list.isError ? (
            <FormError message="Could not load the inbox." testId="inbox-load-error" />
          ) : groups.length === 0 ? (
            <EmptyState
              testId="inbox-empty"
              icon={<Icons.inbox aria-hidden className="size-6" />}
              title="Nothing needs you"
              description="Permission requests, validator verdicts waiting for your decision, stalled task trees, pairing requests and host links that need you show up here."
            />
          ) : (
            groups.map((g) => (
              <section key={g.kind} aria-labelledby={`inbox-group-${g.kind}`} data-testid={`inbox-group-${g.kind}`}>
                <h2 id={`inbox-group-${g.kind}`} className="mb-1.5 px-1 text-ui-sm font-medium text-foreground-subtle">
                  {g.label}
                </h2>
                <ul className="flex flex-col gap-1 rounded-xl border bg-card p-2">
                  {g.items.map((item) => (
                    <InboxRow
                      key={item.key}
                      item={item}
                      automation={automationLinks.get(item.links.taskId ?? item.taskId ?? -1)}
                      narrow={narrow}
                      hostAlias={hostAlias}
                      expanded={expanded === item.key}
                      onToggle={() => setExpanded((cur) => (cur === item.key ? null : item.key))}
                      onOpen={() => open(item)}
                      onOpenHosts={onOpenSettings ? () => openHosts(item) : undefined}
                      onSeen={() => void run(() => actions.seen([item.key]))}
                      onSnooze={(ms) => void run(() => actions.snooze([item], ms))}
                      onResolve={() => void run(() => actions.resolve([item]))}
                    />
                  ))}
                </ul>
              </section>
            ))
          )}
        </div>
      </main>
    </div>
  );
}

/**
 * task id -> automation link, read from the shared workspace-tree cache the sidebar already keeps. Never
 * fetches (`enabled: false`, no queryFn): an inbox opened before the tree loaded just shows no chips.
 */
function useAutomationLinks(): ReadonlyMap<number, { automationId: number; automationName?: string }> {
  const { data } = useQuery<WorkspaceWithTree[], Error, ReadonlyMap<number, { automationId: number; automationName?: string }>>({
    queryKey: queryKeys.workspaceTree(),
    enabled: false,
    select: (tree) => {
      const links = new Map<number, { automationId: number; automationName?: string }>();
      for (const ws of tree) {
        for (const t of [...ws.spaces.flatMap((sp) => sp.tasks), ...ws.ungroupedTasks]) {
          if (t.automationId !== undefined) links.set(t.ID, { automationId: t.automationId, automationName: t.automationName });
        }
      }
      return links;
    },
  });
  return data ?? EMPTY_LINKS;
}

const EMPTY_LINKS: ReadonlyMap<number, { automationId: number; automationName?: string }> = new Map();

function InboxRow({
  item,
  automation,
  narrow,
  hostAlias,
  expanded,
  onToggle,
  onOpen,
  onOpenHosts,
  onSeen,
  onSnooze,
  onResolve,
}: {
  item: InboxItem;
  automation?: { automationId: number; automationName?: string };
  narrow: boolean;
  /** Resolves a fleet host id to its alias; undefined when the host is unknown. */
  hostAlias?: (hostId: string) => string | undefined;
  expanded: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onOpenHosts?: () => void;
  onSeen: () => void;
  onSnooze: (ms: number) => void;
  onResolve: () => void;
}) {
  const permission = item.kind === "permission";
  const btn = narrow ? "h-11" : undefined;
  const hasTask = (item.links.taskId ?? item.taskId) !== undefined;
  const alias = isHostKind(item.kind) && item.links.hostId ? hostAlias?.(item.links.hostId) : undefined;
  return (
    <li
      tabIndex={0}
      data-inbox-row={item.key}
      data-testid={`inbox-row-${item.key}`}
      data-state={item.state}
      data-kind={item.kind}
      aria-label={`${kindLabel(item)}: ${item.title || "task"}. ${item.summary}`}
      className="flex flex-col gap-2 rounded-lg px-2 py-2 outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      <div className="flex items-start gap-3">
        <span
          aria-hidden
          className={cn(
            "mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md bg-muted",
            item.attention === "high" ? "text-warning" : "text-foreground-muted",
          )}
        >
          <KindIcon item={item} />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className={cn("truncate text-ui-base text-foreground", item.state === "new" ? "font-medium" : "font-normal")} data-testid={`inbox-title-${item.key}`}>
              {item.title || "Task"}
            </span>
            <StatusBadge status={tone(item)} data-testid={`inbox-kind-${item.key}`}>
              {kindLabel(item)}
            </StatusBadge>
            {automation && <AutomationChip task={automation} />}
            {alias && <HostChip alias={alias} testId={`inbox-host-chip-${item.key}`} />}
            {item.state === "new" && (
              <span className="text-ui-sm font-medium text-primary" data-testid={`inbox-new-${item.key}`}>
                New
              </span>
            )}
          </span>
          <span className="break-words text-ui-sm text-muted-foreground" data-testid={`inbox-summary-${item.key}`}>
            {item.summary}
          </span>
          <span className="text-ui-sm text-muted-foreground">{formatRelativeTime(item.createdAt)}</span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-1">
        {permission ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={btn}
            aria-expanded={expanded}
            data-testid={`inbox-review-${item.key}`}
            onClick={onToggle}
          >
            {expanded ? "Hide request" : "Review request"}
          </Button>
        ) : isHostKind(item.kind) ? (
          onOpenHosts && (
            <Button type="button" variant="outline" size="sm" className={btn} data-testid={`inbox-hosts-${item.key}`} onClick={onOpenHosts}>
              Review hosts
            </Button>
          )
        ) : (
          hasTask && (
            <Button type="button" variant="outline" size="sm" className={btn} data-testid={`inbox-open-${item.key}`} onClick={onOpen}>
              Open task
            </Button>
          )
        )}
        {/* Always rendered (disabled once seen) so the buttons beside it do not jump when the row is marked. */}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className={btn}
          disabled={item.state !== "new"}
          data-testid={`inbox-seen-${item.key}`}
          onClick={onSeen}
        >
          {item.state === "new" ? "Mark seen" : "Seen"}
        </Button>
        {canSnoozeOrResolve(item) && (
          <>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="ghost" size="sm" className={btn} data-testid={`inbox-snooze-${item.key}`}>
                  <Icons.time aria-hidden className="size-3.5" />
                  Snooze
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                {SNOOZE_OPTIONS.map((o) => (
                  <DropdownMenuItem key={o.id} data-testid={`inbox-snooze-${o.id}-${item.key}`} onSelect={() => onSnooze(o.ms)}>
                    {o.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button type="button" variant="ghost" size="sm" className={btn} data-testid={`inbox-resolve-${item.key}`} onClick={onResolve}>
              <Icons.check aria-hidden className="size-3.5" />
              Resolve
            </Button>
          </>
        )}
      </div>

      {permission && expanded && <RequestCard item={item} onOpenTask={onOpen} />}
    </li>
  );
}

/** The permission card of a `perm:` row: the task page's own component, fed from the run's log. */
function RequestCard({ item, onOpenTask }: { item: InboxItem; onOpenTask: () => void }) {
  const runId = item.links.runId;
  const requestId = item.links.permissionRequestId;
  const request = useInboxRequest(runId, requestId, true);
  const respond = useRespondPermission();
  const [error, setError] = useState<string | null>(null);
  if (request.isPending) return <LoadingRows />;
  if (request.isError) return <FormError message="Could not load the request." testId="inbox-request-error" />;
  if (!request.data || !runId) {
    return (
      <p className="px-1 text-ui-sm text-muted-foreground" data-testid="inbox-request-gone">
        This request was already answered.
      </p>
    );
  }
  return (
    <div data-testid={`inbox-request-${item.key}`} className="flex flex-col gap-2">
      <PermissionCard
        runId={runId}
        pending={request.data}
        onRespond={async (rid, qid, optionId) => {
          setError(null);
          try {
            await respond(rid, qid, optionId);
          } catch (err) {
            setError(err instanceof Error ? err.message : "That did not work.");
            throw err;
          }
        }}
        onChat={onOpenTask}
      />
      {error && <FormError message={error} testId="inbox-respond-error" />}
    </div>
  );
}
