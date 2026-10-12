import { useCallback, useEffect, useId, useMemo, useRef, useState, type JSX, type KeyboardEvent, type ReactNode } from "react";

import { ArrowLeft, Check, ChevronDown, ChevronRight, SquareTerminal } from "lucide-react";

import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { useIsMobile } from "@/hooks/use-mobile";
import {
  filterAndRankModelRows,
  getProviderModelRows,
  resolveInitialModelBrowserView,
  resolveModelBrowserAllView,
  resolveTriggerLabel,
  type BrowserProvider,
  type ModelBrowserView,
  type ModelRow,
} from "@/lib/model-browser";
import { cn } from "@/lib/utils";

// Combined provider + model picker (docs/plans/active/composer-combined-model-picker.md),
// a port of Paseo's combined-model-selector / model-browser interaction:
// root = pinned Profiles + Providers drill-down, provider view = model rows,
// search at both levels. Popover on desktop, bottom Sheet below md.

export interface PickerProfileRow {
  id: string;
  name: string;
  summary: string;
  provider: string;
  notes?: string;
}

export interface ModelPickerProps {
  providers: BrowserProvider[];
  selectedProvider: string;
  selectedModel: string;
  /** Chat bound => only that provider is offered (ADR-0016 P3). */
  boundProvider: string | null;
  disabled: boolean;
  profileRows: PickerProfileRow[];
  /** Applied profile id, sticky even after hand edits. */
  baseAgentId: string | null;
  /** Fields hand-edited away from the applied profile. */
  custom: boolean;
  onSelect: (provider: string, modelId: string) => void;
  onApplyProfile: (profileId: string) => void;
  onClearAgent: () => void;
  /** Opens Settings -> Agents. */
  onManageAgents: () => void;
  /** Fired each time the picker opens (the owner lazily fetches provider health). */
  onOpen?: () => void;
}

/** One selectable row of the current view, in display order. */
type PickerRow =
  | { kind: "noAgent"; key: string }
  | { kind: "profile"; key: string; profile: PickerProfileRow }
  | { kind: "manage"; key: string }
  | { kind: "provider"; key: string; provider: BrowserProvider }
  | { kind: "model"; key: string; row: ModelRow; qualified: boolean };

type PickerEntry = { kind: "heading"; key: string; label: string } | PickerRow;

type BodyState =
  | { kind: "entries"; entries: PickerEntry[] }
  | { kind: "empty"; query: string };

function isDisabledRow(row: PickerRow): boolean {
  return row.kind === "provider" && !!row.provider.unavailableReason;
}

function profileEntries(profiles: PickerProfileRow[], baseAgentId: string | null): PickerEntry[] {
  const entries: PickerEntry[] = [{ kind: "heading", key: "h-profiles", label: "Profiles" }];
  if (baseAgentId !== null) {
    entries.push({ kind: "noAgent", key: "no-agent" });
  }
  for (const profile of profiles) {
    entries.push({ kind: "profile", key: `profile-${profile.id}`, profile });
  }
  entries.push({ kind: "manage", key: "manage" });
  return entries;
}

function modelEntries(rows: ModelRow[], qualified: boolean): PickerEntry[] {
  return rows.map((row) => ({ kind: "model", key: `model-${row.key}`, row, qualified }));
}

export function ModelPicker(props: ModelPickerProps): JSX.Element {
  const {
    providers,
    selectedProvider,
    selectedModel,
    boundProvider,
    disabled,
    profileRows,
    baseAgentId,
    custom,
    onSelect,
    onApplyProfile,
    onClearAgent,
    onManageAgents,
    onOpen,
  } = props;
  const isMobile = useIsMobile();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [view, setView] = useState<ModelBrowserView>({ kind: "all" });
  const inputRef = useRef<HTMLInputElement>(null);

  const visibleProviders = useMemo(
    () => (boundProvider ? providers.filter((p) => p.id === boundProvider) : providers),
    [providers, boundProvider],
  );
  const label = resolveTriggerLabel({ providers, selectedProvider, selectedModel });
  const canGoBack = view.kind === "provider" && visibleProviders.length > 1;
  const normalizedQuery = query.trim().toLowerCase();

  const changeOpen = useCallback(
    (next: boolean) => {
      setOpen(next);
      if (!next) {
        return;
      }
      setQuery("");
      setActiveIndex(0);
      setView(
        resolveInitialModelBrowserView({
          providers: visibleProviders,
          selectedProvider,
          selectedModel,
          hasProfiles: profileRows.length > 0,
        }),
      );
      onOpen?.();
    },
    [visibleProviders, selectedProvider, selectedModel, profileRows.length, onOpen],
  );

  const body = useMemo<BodyState>(() => {
    if (view.kind === "provider") {
      const provider = visibleProviders.find((p) => p.id === view.providerId);
      const rows = provider ? getProviderModelRows(provider) : [];
      if (normalizedQuery) {
        const matches = filterAndRankModelRows(rows, normalizedQuery);
        return matches.length === 0
          ? { kind: "empty", query: query.trim() }
          : { kind: "entries", entries: modelEntries(matches, false) };
      }
      const profiles = profileRows.filter((p) => p.provider === view.providerId);
      const entries: PickerEntry[] =
        profiles.length > 0 || baseAgentId !== null ? profileEntries(profiles, baseAgentId) : [];
      if (entries.length > 0) {
        entries.push({ kind: "heading", key: "h-models", label: "Models" });
      }
      return { kind: "entries", entries: [...entries, ...modelEntries(rows, false)] };
    }

    const all = resolveModelBrowserAllView({
      providers: visibleProviders,
      normalizedQuery,
      isSearchFocused: false,
    });
    if (all.kind === "noSearchMatches") {
      return { kind: "empty", query: query.trim() };
    }
    if (all.kind === "searchResults") {
      return { kind: "entries", entries: modelEntries(all.rows, true) };
    }
    const entries: PickerEntry[] =
      profileRows.length > 0 ? profileEntries(profileRows, baseAgentId) : [];
    if (visibleProviders.length > 0) {
      entries.push({ kind: "heading", key: "h-providers", label: "Providers" });
    }
    for (const provider of visibleProviders) {
      entries.push({ kind: "provider", key: `provider-${provider.id}`, provider });
    }
    return { kind: "entries", entries };
  }, [view, visibleProviders, normalizedQuery, query, profileRows, baseAgentId]);

  // Arrow keys walk only the rows that can be activated; disabled providers are skipped.
  const actionable = useMemo<PickerRow[]>(
    () =>
      body.kind === "entries"
        ? body.entries.filter((e): e is PickerRow => e.kind !== "heading" && !isDisabledRow(e))
        : [],
    [body],
  );
  const safeIndex = actionable.length === 0 ? -1 : Math.min(activeIndex, actionable.length - 1);
  const activeKey = safeIndex >= 0 ? actionable[safeIndex]!.key : null;
  const rowDomId = (key: string) => `${listId}-${key}`;

  useEffect(() => {
    if (!open || !activeKey) {
      return;
    }
    document.getElementById(`${listId}-${activeKey}`)?.scrollIntoView?.({ block: "nearest" });
  }, [open, activeKey, listId]);

  const activate = (row: PickerRow) => {
    switch (row.kind) {
      case "model":
        changeOpen(false);
        onSelect(row.row.provider, row.row.modelId);
        return;
      case "provider":
        if (isDisabledRow(row)) {
          return;
        }
        setView({ kind: "provider", providerId: row.provider.id, providerLabel: row.provider.label });
        setQuery("");
        setActiveIndex(0);
        return;
      case "profile":
        changeOpen(false);
        onApplyProfile(row.profile.id);
        return;
      case "noAgent":
        changeOpen(false);
        onClearAgent();
        return;
      case "manage":
        changeOpen(false);
        onManageAgents();
        return;
    }
  };

  const goBack = () => {
    setView({ kind: "all" });
    setQuery("");
    setActiveIndex(0);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) {
      return;
    }
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (actionable.length === 0) {
        return;
      }
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActiveIndex((Math.max(safeIndex, 0) + step + actionable.length) % actionable.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const row = safeIndex >= 0 ? actionable[safeIndex] : undefined;
      if (row) {
        activate(row);
      }
    } else if ((e.key === "ArrowLeft" || e.key === "Backspace") && query === "" && canGoBack) {
      e.preventDefault();
      goBack();
    }
  };

  const inProviderView = view.kind === "provider";
  const boundLabel = boundProvider ? (providers.find((p) => p.id === boundProvider)?.label ?? boundProvider) : null;

  const content = (
    <>
      <PickerHeader
        title={view.kind === "provider" ? view.providerLabel : "Select model"}
        onBack={canGoBack ? goBack : undefined}
        inSheet={isMobile}
      />
      {boundLabel && (
        // ADR-0016 P3: a bound chat cannot switch providers, so say why the list is short.
        <p className="px-3 pb-2 text-ui-sm text-foreground-muted">
          Bound to {boundLabel} — start a new chat to switch providers
        </p>
      )}
      <div className="border-b px-3 pb-2">
        <input
          ref={inputRef}
          autoFocus
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActiveIndex(0);
          }}
          onKeyDown={onKeyDown}
          aria-label="Search models"
          placeholder={inProviderView ? "Search models…" : "Search all models…"}
          data-testid="model-search-input"
          role="combobox"
          aria-expanded
          aria-controls={listId}
          aria-activedescendant={activeKey ? rowDomId(activeKey) : undefined}
          autoComplete="off"
          spellCheck={false}
          className="h-8 w-full rounded-lg border border-input-border bg-transparent px-2.5 text-ui-sm text-foreground outline-none placeholder:text-foreground-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
        />
      </div>
      <div id={listId} role="listbox" aria-label="Models" className="min-h-0 flex-1 overflow-y-auto p-1">
        {body.kind === "empty" ? (
          <EmptyState query={body.query} />
        ) : (
          body.entries.map((entry) => {
            if (entry.kind === "heading") {
              return (
                <div
                  key={entry.key}
                  role="presentation"
                  className="px-2 pt-2 pb-1 text-ui-sm font-medium text-foreground-muted"
                >
                  {entry.label}
                </div>
              );
            }
            const active = entry.key === activeKey;
            const rowProps: RowShellProps = {
              id: rowDomId(entry.key),
              active,
              disabled: isDisabledRow(entry),
              onActivate: () => activate(entry),
              onHover: () => {
                const idx = actionable.findIndex((r) => r.key === entry.key);
                if (idx >= 0) {
                  setActiveIndex(idx);
                }
              },
            };
            switch (entry.kind) {
              case "noAgent":
                return (
                  <RowShell key={entry.key} {...rowProps} testId="model-no-agent">
                    <span className="text-ui-base text-foreground">No agent</span>
                  </RowShell>
                );
              case "manage":
                return (
                  <RowShell key={entry.key} {...rowProps} testId="model-manage-agents">
                    <span className="text-ui-base text-foreground-muted">Manage agents…</span>
                  </RowShell>
                );
              case "profile":
                return (
                  <ProfileRow
                    key={entry.key}
                    {...rowProps}
                    profile={entry.profile}
                    checked={String(entry.profile.id) === baseAgentId}
                    custom={custom}
                  />
                );
              case "provider":
                return <ProviderRow key={entry.key} {...rowProps} provider={entry.provider} />;
              case "model":
                return (
                  <ModelRowItem
                    key={entry.key}
                    {...rowProps}
                    row={entry.row}
                    qualified={entry.qualified}
                    selected={entry.row.provider === selectedProvider && entry.row.modelId === selectedModel}
                  />
                );
            }
          })
        )}
      </div>
    </>
  );

  const trigger = (
    <Button
      type="button"
      variant="ghost"
      data-testid="composer-model-picker"
      aria-label={`Model: ${label}`}
      title={label}
      disabled={disabled}
      className={PILL_TRIGGER_CLASS}
      onClick={isMobile ? () => changeOpen(!open) : undefined}
    >
      <SquareTerminal aria-hidden className="shrink-0" />
      <span className="max-sm:hidden">{label}</span>
      <ChevronDown aria-hidden className="shrink-0" />
    </Button>
  );

  // The search input owns focus (it keeps it while arrowing), so Radix's own
  // focus-the-container-on-open is redirected to it.
  const focusInput = (e: Event) => {
    e.preventDefault();
    inputRef.current?.focus();
  };

  if (isMobile) {
    return (
      <>
        {trigger}
        <Sheet open={open} onOpenChange={changeOpen}>
          <SheetContent
            side="bottom"
            className="h-[75vh] gap-0 p-0"
            data-testid="model-picker-sheet"
            onOpenAutoFocus={focusInput}
          >
            <SheetTitle className="sr-only">Select model</SheetTitle>
            <SheetDescription className="sr-only">Choose a provider and model</SheetDescription>
            {content}
          </SheetContent>
        </Sheet>
      </>
    );
  }

  return (
    <Popover open={open} onOpenChange={changeOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        className="flex h-[min(26rem,var(--radix-popover-content-available-height))] w-[22rem] flex-col p-0"
        data-testid="model-picker-popover"
        onOpenAutoFocus={focusInput}
      >
        {content}
      </PopoverContent>
    </Popover>
  );
}

function PickerHeader({ title, onBack, inSheet }: { title: string; onBack?: () => void; inSheet: boolean }) {
  return (
    <div className={cn("flex h-10 shrink-0 items-center gap-1 px-2", inSheet && "pr-12")}>
      {onBack && (
        <Button
          type="button"
          variant="quiet"
          size="icon-sm"
          aria-label="Back"
          data-testid="model-picker-back"
          onClick={onBack}
        >
          <ArrowLeft aria-hidden />
        </Button>
      )}
      <span className={cn("truncate text-ui-sm font-medium text-foreground", !onBack && "px-1")}>{title}</span>
    </div>
  );
}

interface RowShellProps {
  id: string;
  active: boolean;
  disabled: boolean;
  onActivate: () => void;
  onHover: () => void;
}

/** Shared option chrome: id for aria-activedescendant, active highlight, hover-to-activate. */
function RowShell({
  id,
  active,
  disabled,
  onActivate,
  onHover,
  testId,
  selected,
  children,
}: RowShellProps & { testId: string; selected?: boolean; children: ReactNode }) {
  return (
    <div
      id={id}
      role="option"
      aria-selected={selected ?? false}
      aria-disabled={disabled || undefined}
      data-testid={testId}
      data-active={active ? "true" : undefined}
      onClick={disabled ? undefined : onActivate}
      onMouseMove={disabled || active ? undefined : onHover}
      className={cn(
        "flex min-h-9 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5",
        active && "bg-hover",
        disabled && "cursor-not-allowed opacity-50",
      )}
    >
      {children}
    </div>
  );
}

function SelectedCheck({ show }: { show: boolean }) {
  return show ? <Check aria-hidden className="size-4 shrink-0 text-foreground" /> : null;
}

function ProfileRow({
  profile,
  checked,
  custom,
  ...shell
}: RowShellProps & { profile: PickerProfileRow; checked: boolean; custom: boolean }) {
  return (
    <RowShell {...shell} testId={`model-profile-row-${profile.id}`} selected={checked}>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-ui-base text-foreground">{profile.name}</span>
          {checked && custom && (
            <span className="shrink-0 rounded bg-muted px-1 text-ui-xs text-foreground-muted">custom</span>
          )}
        </div>
        <div className="truncate text-ui-sm text-foreground-muted">{profile.summary}</div>
      </div>
      <SelectedCheck show={checked} />
    </RowShell>
  );
}

function ProviderRow({ provider, ...shell }: RowShellProps & { provider: BrowserProvider }) {
  const selection = provider.modelSelection;
  const count = getProviderModelRows(provider).length;
  let detail: string;
  if (provider.unavailableReason) {
    detail = provider.unavailableReason;
  } else if (selection.kind === "error") {
    detail = selection.message;
  } else if (selection.kind === "loading") {
    detail = "Loading…";
  } else {
    detail = count === 1 ? "1 model" : `${count} models`;
  }
  return (
    <RowShell {...shell} testId={`model-provider-${provider.id}`}>
      <div className="min-w-0 flex-1">
        <div className="truncate text-ui-base text-foreground">{provider.label}</div>
        <div className="truncate text-ui-sm text-foreground-muted">{detail}</div>
      </div>
      {!shell.disabled && <ChevronRight aria-hidden className="size-4 shrink-0 text-foreground-muted" />}
    </RowShell>
  );
}

function ModelRowItem({
  row,
  qualified,
  selected,
  ...shell
}: RowShellProps & { row: ModelRow; qualified: boolean; selected: boolean }) {
  // ADR-0025: the "Default model" row (id "") sends no model; it has no description of its own.
  const description = qualified
    ? row.description
      ? `${row.providerLabel} · ${row.description}`
      : row.providerLabel
    : row.description;
  return (
    <RowShell {...shell} testId={`model-row-${row.key}`} selected={selected}>
      <div className="min-w-0 flex-1">
        <div className="truncate text-ui-base text-foreground">{row.modelLabel}</div>
        {description && <div className="truncate text-ui-sm text-foreground-muted">{description}</div>}
      </div>
      <SelectedCheck show={selected} />
    </RowShell>
  );
}

function EmptyState({ query }: { query: string }) {
  return (
    <div data-testid="model-search-empty" className="px-3 py-6 text-center text-ui-sm text-foreground-muted">
      No models match “{query}”
    </div>
  );
}
