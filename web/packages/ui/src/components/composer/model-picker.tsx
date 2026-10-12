import { useCallback, useEffect, useId, useMemo, useRef, useState, type JSX, type KeyboardEvent } from "react";

import { ChevronDown } from "lucide-react";

import {
  CreateProfileRow,
  EmptyState,
  ModelRowItem,
  NoAgentRow,
  PickerHeader,
  ProfileRow,
  ProfilesHeading,
  ProviderNotice,
  ProviderRow,
  SectionHeading,
  type ModelRowAction,
  type PickerProfileRow,
  type RowShellProps,
} from "@/components/composer/model-picker-rows";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import { ProviderGlyph } from "@/components/composer/provider-glyph";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { useIsMobile } from "@/hooks/use-mobile";
import {
  filterAndRankModelRows,
  getProviderModelRows,
  groupProfilesByProviderModel,
  resolveDesktopFixedHeight,
  resolveInitialModelBrowserView,
  resolveModelBrowserAllView,
  resolveTriggerLabel,
  resolveTriggerTitle,
  type BrowserProvider,
  type ModelBrowserView,
  type ModelRow,
} from "@/lib/model-browser";
import type { ProfileSeed } from "@/lib/settings-intent";

// Combined provider + model picker (docs/plans/active/model-picker-paseo-parity.md),
// a port of Paseo's combined-model-selector / model-browser: opens on the
// current provider's view, root = Profiles band + Providers drill-down, search
// at both levels. Popover on desktop, bottom Sheet below md.

export type { PickerProfileRow };

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
  /** Live run config; seeds "create profile" from the selected model row. */
  currentConfig?: { permissionMode: string; autoAccept: boolean; thinkingLevel: string };
  onSelect: (provider: string, modelId: string) => void;
  onApplyProfile: (profileId: string) => void;
  onClearAgent: () => void;
  /** Opens Settings -> Agents. */
  onManageAgents: () => void;
  onCreateProfile?: (seed: ProfileSeed) => void;
  onEditProfile?: (profileId: string) => void;
  onOpenProviderSettings?: (providerId: string) => void;
  /** Fired each time the picker opens (the owner lazily fetches provider health). */
  onOpen?: () => void;
}

/** One selectable row of the current view, in display order. */
type PickerRow =
  | { kind: "noAgent"; key: string }
  | { kind: "profile"; key: string; profile: PickerProfileRow }
  | { kind: "createProfile"; key: string }
  | { kind: "provider"; key: string; provider: BrowserProvider }
  | { kind: "model"; key: string; row: ModelRow; qualified: boolean };

type PickerEntry =
  | { kind: "heading"; key: string; label: string }
  | { kind: "profilesHeading"; key: string }
  | PickerRow;

type BodyState =
  | {
      kind: "entries";
      /** Tinted Profiles band, above the list. */
      band: PickerEntry[];
      entries: PickerEntry[];
      notice?: { kind: "loading" } | { kind: "error"; message: string };
    }
  | { kind: "empty"; query: string };

function isDisabledRow(row: PickerRow): boolean {
  return row.kind === "provider" && !!row.provider.unavailableReason;
}

function bandEntries(profiles: PickerProfileRow[], baseAgentId: string | null): PickerEntry[] {
  const entries: PickerEntry[] = [{ kind: "profilesHeading", key: "h-profiles" }];
  if (baseAgentId !== null) {
    entries.push({ kind: "noAgent", key: "no-agent" });
  }
  for (const profile of profiles) {
    entries.push({ kind: "profile", key: `profile-${profile.id}`, profile });
  }
  if (profiles.length === 0) {
    entries.push({ kind: "createProfile", key: "create-profile" });
  }
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
    currentConfig,
    onSelect,
    onApplyProfile,
    onClearAgent,
    onManageAgents,
    onCreateProfile,
    onEditProfile,
    onOpenProviderSettings,
    onOpen,
  } = props;
  const isMobile = useIsMobile();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [view, setView] = useState<ModelBrowserView>({ kind: "all" });
  const [inputFocused, setInputFocused] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const visibleProviders = useMemo(
    () => (boundProvider ? providers.filter((p) => p.id === boundProvider) : providers),
    [providers, boundProvider],
  );
  const triggerArgs = { providers, selectedProvider, selectedModel };
  const label = resolveTriggerLabel(triggerArgs);
  const title = resolveTriggerTitle(triggerArgs);
  const canGoBack = view.kind === "provider" && visibleProviders.length > 1;
  const normalizedQuery = query.trim().toLowerCase();
  // On the sheet the keyboard covers the list, so focusing an empty search shows every model (Paseo searchAllOnFocus).
  const searchFocused = isMobile && inputFocused;
  const profilesByModel = useMemo(
    () => groupProfilesByProviderModel(profileRows.map((p) => ({ ...p, modelId: p.model }))),
    [profileRows],
  );

  const changeOpen = useCallback(
    (next: boolean) => {
      setOpen(next);
      setInputFocused(false);
      if (!next) {
        return;
      }
      setQuery("");
      setActiveIndex(0);
      setView(resolveInitialModelBrowserView({ providers: visibleProviders, selectedProvider }));
      onOpen?.();
    },
    [visibleProviders, selectedProvider, onOpen],
  );

  const body = useMemo<BodyState>(() => {
    if (view.kind === "provider") {
      const provider = visibleProviders.find((p) => p.id === view.providerId);
      const selection = provider?.modelSelection;
      if (selection?.kind === "loading" || selection?.kind === "error") {
        return {
          kind: "entries",
          band: [],
          entries: [],
          notice: selection.kind === "loading" ? { kind: "loading" } : { kind: "error", message: selection.message },
        };
      }
      const rows = provider ? getProviderModelRows(provider) : [];
      if (normalizedQuery) {
        const matches = filterAndRankModelRows(rows, normalizedQuery);
        return matches.length === 0
          ? { kind: "empty", query: query.trim() }
          : { kind: "entries", band: [], entries: modelEntries(matches, false) };
      }
      const profiles = profileRows.filter((p) => p.provider === view.providerId);
      return {
        kind: "entries",
        band: bandEntries(profiles, baseAgentId),
        entries: [{ kind: "heading", key: "h-models", label: "Models" }, ...modelEntries(rows, false)],
      };
    }

    const all = resolveModelBrowserAllView({
      providers: visibleProviders,
      normalizedQuery,
      isSearchFocused: searchFocused,
    });
    if (all.kind === "noSearchMatches") {
      return { kind: "empty", query: query.trim() };
    }
    if (all.kind === "searchResults") {
      return { kind: "entries", band: [], entries: modelEntries(all.rows, true) };
    }
    const entries: PickerEntry[] = [];
    if (visibleProviders.length > 0) {
      entries.push({ kind: "heading", key: "h-providers", label: "Providers" });
    }
    for (const provider of visibleProviders) {
      entries.push({ kind: "provider", key: `provider-${provider.id}`, provider });
    }
    return { kind: "entries", band: bandEntries(profileRows, baseAgentId), entries };
  }, [view, visibleProviders, normalizedQuery, query, searchFocused, profileRows, baseAgentId]);

  // Popover height follows the browse layout of the view, not the search results, so typing doesn't resize it.
  const extraRows = useMemo(() => {
    const profiles = view.kind === "provider" ? profileRows.filter((p) => p.provider === view.providerId) : profileRows;
    // Heading + No agent + (profiles | Create row) + the Models/Providers heading.
    return 1 + (baseAgentId !== null ? 1 : 0) + Math.max(profiles.length, 1) + 1;
  }, [view, profileRows, baseAgentId]);
  const popoverHeight = resolveDesktopFixedHeight(view, visibleProviders, extraRows);

  // Arrow keys walk only the rows that can be activated; headings, disabled providers and row action buttons are skipped.
  const actionable = useMemo<PickerRow[]>(
    () =>
      body.kind === "entries"
        ? [...body.band, ...body.entries].filter(
            (e): e is PickerRow => e.kind !== "heading" && e.kind !== "profilesHeading" && !isDisabledRow(e),
          )
        : [],
    [body],
  );
  const safeIndex = actionable.length === 0 ? -1 : Math.min(activeIndex, actionable.length - 1);
  const activeKey = safeIndex >= 0 ? actionable[safeIndex]!.key : null;
  const rowDomId = (key: string) => `${listId}-${key}`;

  // Start the highlight on the current selection (Paseo scrolls to it) rather
  // than the first row, each time the picker opens or changes view.
  useEffect(() => {
    if (!open) {
      return;
    }
    const at = actionable.findIndex(
      (r) => r.kind === "model" && r.row.provider === selectedProvider && r.row.modelId === selectedModel,
    );
    setActiveIndex(at >= 0 ? at : 0);
    // Deliberately keyed on open/view only: typing resets the index itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, view]);

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
      case "createProfile":
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

  const manageAgents = () => {
    changeOpen(false);
    onManageAgents();
  };

  const modelAction = (row: ModelRow): { action?: ModelRowAction; run: () => void } => {
    // The synthetic default row (id "") names no model, so nothing to build a profile from.
    if (row.modelId === "") {
      return { run: () => {} };
    }
    const existing = profilesByModel.get(`${row.provider}:${row.modelId}`) ?? [];
    if (existing.length === 0) {
      if (!onCreateProfile) {
        return { run: () => {} };
      }
      const isSelected = row.provider === selectedProvider && row.modelId === selectedModel;
      return {
        action: { kind: "create" },
        run: () => {
          changeOpen(false);
          onCreateProfile({
            name: row.modelLabel,
            provider: row.provider,
            model: row.modelId,
            ...(isSelected ? currentConfig : {}),
          });
        },
      };
    }
    if (existing.length === 1 && onEditProfile) {
      const only = existing[0]!;
      return {
        action: { kind: "edit", profile: only },
        run: () => {
          changeOpen(false);
          onEditProfile(only.id);
        },
      };
    }
    return { action: { kind: "list", count: existing.length }, run: manageAgents };
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

  const shellFor = (entry: PickerRow): RowShellProps => ({
    id: rowDomId(entry.key),
    active: entry.key === activeKey,
    disabled: isDisabledRow(entry),
    onActivate: () => activate(entry),
    onHover: () => {
      const idx = actionable.findIndex((r) => r.key === entry.key);
      if (idx >= 0) {
        setActiveIndex(idx);
      }
    },
  });

  const renderEntry = (entry: PickerEntry, index: number) => {
    switch (entry.kind) {
      case "heading":
        return <SectionHeading key={entry.key} label={entry.label} />;
      case "profilesHeading":
        return <ProfilesHeading key={entry.key} onEdit={manageAgents} />;
      case "noAgent":
        return <NoAgentRow key={entry.key} {...shellFor(entry)} />;
      case "createProfile":
        return <CreateProfileRow key={entry.key} {...shellFor(entry)} />;
      case "profile":
        return (
          <ProfileRow
            key={entry.key}
            {...shellFor(entry)}
            profile={entry.profile}
            checked={String(entry.profile.id) === baseAgentId}
            custom={custom}
          />
        );
      case "provider":
        return <ProviderRow key={entry.key} {...shellFor(entry)} provider={entry.provider} separator={index > 1} />;
      case "model": {
        const { action, run } = modelAction(entry.row);
        return (
          <ModelRowItem
            key={entry.key}
            {...shellFor(entry)}
            row={entry.row}
            qualified={entry.qualified}
            selected={entry.row.provider === selectedProvider && entry.row.modelId === selectedModel}
            action={action}
            onAction={run}
          />
        );
      }
    }
  };

  const content = (
    <>
      <PickerHeader
        title={view.kind === "provider" ? view.providerLabel : "Select model"}
        providerId={view.kind === "provider" ? view.providerId : undefined}
        onBack={canGoBack ? goBack : undefined}
        onProviderSettings={
          view.kind === "provider" && onOpenProviderSettings
            ? () => {
                changeOpen(false);
                onOpenProviderSettings(view.providerId);
              }
            : undefined
        }
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
          autoFocus={!isMobile}
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActiveIndex(0);
          }}
          onFocus={() => {
            setInputFocused(true);
            // Only the sheet's list changes on focus; resetting on desktop would drop the highlight on the current selection.
            if (isMobile) {
              setActiveIndex(0);
            }
          }}
          onBlur={() => setInputFocused(false)}
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
          className="h-8 w-full rounded-lg border border-input-border bg-transparent px-2.5 text-ui-sm text-foreground outline-none transition-colors placeholder:text-foreground-muted focus-visible:border-input-border-focused max-md:text-ui-lg"
        />
      </div>
      <div id={listId} role="listbox" aria-label="Models" className="min-h-0 flex-1 overflow-y-auto">
        {body.kind === "empty" ? (
          <EmptyState query={body.query} />
        ) : (
          <>
            {body.band.length > 0 && (
              <div data-testid="model-profiles-band" className="border-b border-border bg-surface p-1">
                {body.band.map(renderEntry)}
              </div>
            )}
            {body.notice ? (
              <ProviderNotice kind={body.notice.kind} message={body.notice.kind === "error" ? body.notice.message : undefined} />
            ) : (
              <div className="p-1">{body.entries.map(renderEntry)}</div>
            )}
          </>
        )}
      </div>
    </>
  );

  const trigger = (
    <Button
      type="button"
      variant="ghost"
      data-testid="composer-model-picker"
      aria-label={`Model: ${title}`}
      title={title}
      disabled={disabled}
      className={PILL_TRIGGER_CLASS}
      onClick={isMobile ? () => changeOpen(!open) : undefined}
    >
      <ProviderGlyph provider={selectedProvider} className="shrink-0" />
      <span className="max-w-[12rem] min-w-0 truncate max-sm:hidden">{label}</span>
      <ChevronDown aria-hidden className="shrink-0" />
    </Button>
  );

  // On desktop the search input owns focus (it keeps it while arrowing), so
  // Radix's own focus-the-container-on-open is redirected to it. The sheet
  // leaves focus alone: a focused input would pop the keyboard over the list.
  const focusInput = (e: Event) => {
    e.preventDefault();
    if (!isMobile) {
      inputRef.current?.focus();
    }
  };

  if (isMobile) {
    return (
      <>
        {trigger}
        <Sheet open={open} onOpenChange={changeOpen}>
          <SheetContent
            side="bottom"
            className="h-[80dvh] gap-0 p-0"
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
        className="flex w-[22rem] flex-col p-0"
        style={{ height: `min(${popoverHeight}px, var(--radix-popover-content-available-height))` }}
        data-testid="model-picker-popover"
        onOpenAutoFocus={focusInput}
      >
        {content}
      </PopoverContent>
    </Popover>
  );
}
