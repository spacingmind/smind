import { useCallback, useEffect, useId, useMemo, useRef, useState, type JSX, type KeyboardEvent } from "react";

import { Icons } from "@/lib/icons";
import { providerLabel } from "@/lib/provider-catalog";

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
  UnavailableNotice,
  type ModelRowAction,
  type PickerProfileRow,
  type RowShellProps,
} from "@/components/pickers/provider-model-picker-rows";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import { ProviderIcon } from "@/components/provider-icon";
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

// Combined provider + model picker (docs/plans/completed/model-picker-paseo-parity.md),
// a port of Paseo's combined-model-selector / model-browser: opens on the
// current provider's view, root = Profiles band + Providers drill-down, search
// at both levels. Popover on desktop, bottom Sheet below md.

export type { PickerProfileRow };

export interface ProviderModelPickerProps {
  providers: BrowserProvider[];
  selectedProvider: string;
  selectedModel: string;
  /** The model the latest run reported: names the chip when selectedModel is "" and no default is configured or catalogued. */
  observedModel?: string;
  /** Chat bound => only that provider is offered (ADR-0016 P3). */
  boundProvider: string | null;
  /** The line shown while bound (default: "Bound to <provider> — start a new chat to switch providers"); `null` hides it, for an owner that is not a chat (Settings -> Providers). */
  boundNotice?: string | null;
  disabled: boolean;
  /**
   * "pill" is the composer toolbar's compact trigger; "field" is a full-width form control (Settings -> Agents'
   * profile form) that opens downwards. Same popover, same rows, same health-disabled providers.
   */
  variant?: "pill" | "field";
  /** The trigger's data-testid (the composer keeps "composer-model-picker"). */
  testId?: string;
  /**
   * Profiles ("agents") band, the create/edit-profile affordances and "No agent": present only when the owner
   * can apply profiles (`onApplyProfile` set) -- a profile *form* picks a provider/model and nothing else.
   */
  profileRows?: PickerProfileRow[];
  /** Applied profile id, sticky even after hand edits. */
  baseAgentId?: string | null;
  /** Fields hand-edited away from the applied profile. */
  custom?: boolean;
  /** Live run config; seeds "create profile" from the selected model row. */
  currentConfig?: { permissionMode: string; autoAccept: boolean; thinkingLevel: string };
  onSelect: (provider: string, modelId: string) => void;
  onApplyProfile?: (profileId: string) => void;
  onClearAgent?: () => void;
  /** Opens Settings -> Agents. */
  onManageAgents?: () => void;
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

/** A stable empty list (a fresh `[]` default per render would re-run every memo keyed on it). */
const NO_PROFILE_ROWS: PickerProfileRow[] = [];

function hasFinePointer(): boolean {
  return window.matchMedia?.("(pointer: fine)").matches ?? false;
}

export function ProviderModelPicker(props: ProviderModelPickerProps): JSX.Element {
  const {
    providers,
    selectedProvider,
    selectedModel,
    observedModel,
    boundProvider,
    boundNotice,
    disabled,
    variant = "pill",
    testId = "composer-model-picker",
    profileRows = NO_PROFILE_ROWS,
    baseAgentId = null,
    custom = false,
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
  const triggerArgs = { providers, selectedProvider, selectedModel, observedModel };
  const label = resolveTriggerLabel(triggerArgs);
  const title = resolveTriggerTitle(triggerArgs);
  const canGoBack = view.kind === "provider" && visibleProviders.length > 1;
  const hasProfiles = onApplyProfile !== undefined;
  const normalizedQuery = query.trim().toLowerCase();
  // On the sheet the keyboard covers the list, so focusing an empty search shows every model (Paseo searchAllOnFocus).
  // A fine pointer (a narrow desktop window) has no on-screen keyboard and autofocuses the input, so it keeps the browse layout.
  const searchFocused = isMobile && inputFocused && !hasFinePointer();
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
        band: hasProfiles ? bandEntries(profiles, baseAgentId) : [],
        entries: [{ kind: "heading", key: "h-models", label: "Models" }, ...modelEntries(rows, false)],
      };
    }

    const all = resolveModelBrowserAllView({
      providers: visibleProviders,
      normalizedQuery,
      isSearchFocused: searchFocused,
    });
    if (all.kind === "noSearchMatches") {
      // A loading provider may still contribute rows, so an empty result is not final yet.
      return visibleProviders.some((p) => p.modelSelection.kind === "loading")
        ? { kind: "entries", band: [], entries: [], notice: { kind: "loading" } }
        : { kind: "empty", query: query.trim() };
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
    return { kind: "entries", band: hasProfiles ? bandEntries(profileRows, baseAgentId) : [], entries };
  }, [view, visibleProviders, normalizedQuery, query, searchFocused, profileRows, baseAgentId, hasProfiles]);

  // Popover height follows the browse layout of the view, not the search results, so typing doesn't resize it.
  const extraRows = useMemo(() => {
    const profiles = view.kind === "provider" ? profileRows.filter((p) => p.provider === view.providerId) : profileRows;
    // Heading + No agent + (profiles | Create row) + the Models/Providers heading.
    if (!hasProfiles) return 1 + (boundProvider ? 1 : 0);
    return 1 + (baseAgentId !== null ? 1 : 0) + Math.max(profiles.length, 1) + 1 + (boundProvider ? 1 : 0);
  }, [view, profileRows, baseAgentId, boundProvider, hasProfiles]);
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
        onApplyProfile?.(row.profile.id);
        return;
      case "noAgent":
        changeOpen(false);
        onClearAgent?.();
        return;
      case "createProfile":
        changeOpen(false);
        if (view.kind === "provider" && onCreateProfile) {
          onCreateProfile({ name: "", provider: view.providerId, model: "" });
        } else {
          onManageAgents?.();
        }
        return;
    }
  };

  const goBack = () => {
    setView({ kind: "all" });
    setQuery("");
    setActiveIndex(0);
    // Keep keyboard flow on desktop; on touch this would raise the keyboard over the list.
    if (!isMobile || hasFinePointer()) {
      inputRef.current?.focus();
    }
  };

  const manageAgents = () => {
    changeOpen(false);
    onManageAgents?.();
  };

  const modelAction = (row: ModelRow): { action?: ModelRowAction; run: () => void } => {
    // The synthetic default row (id "") names no model, so nothing to build a profile from.
    if (row.modelId === "") {
      return { run: () => {} };
    }
    if (!hasProfiles) {
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
    // A plugin-owned agent cannot be edited: its row offers the list (Settings -> Agents, where it is labelled) instead.
    if (existing.length === 1 && onEditProfile && !existing[0]!.owner) {
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
  const viewProvider = view.kind === "provider" ? visibleProviders.find((p) => p.id === view.providerId) : undefined;
  const boundLabel = boundProvider ? providerLabel(boundProvider, providers.filter((p) => p.id === boundProvider)) : null;

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
      {boundLabel && boundNotice !== null && (
        // ADR-0016 P3: a bound chat cannot switch providers, so say why the list is short.
        <p className="px-3 pb-2 text-ui-sm text-foreground-muted">
          {boundNotice ?? `Bound to ${boundLabel} — start a new chat to switch providers`}
        </p>
      )}
      {viewProvider?.unavailableReason && <UnavailableNotice reason={viewProvider.unavailableReason} />}
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

  const trigger =
    variant === "pill" ? (
      <Button
        type="button"
        variant="ghost"
        data-testid={testId}
        aria-label={`Model: ${title}`}
        title={title}
        disabled={disabled}
        className={PILL_TRIGGER_CLASS}
        onClick={isMobile ? () => changeOpen(!open) : undefined}
      >
        <ProviderIcon provider={selectedProvider} className="shrink-0" />
        <span className="max-w-[12rem] min-w-0 truncate max-sm:hidden">{label}</span>
        <Icons.chevronDown aria-hidden className="shrink-0" />
      </Button>
    ) : (
      // Looks like the Settings form's Select trigger (components/ui/select.tsx), so a form row reads as one field.
      <button
        type="button"
        data-testid={testId}
        aria-label={`Model: ${title}`}
        title={title}
        disabled={disabled}
        className="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-lg whitespace-nowrap transition-colors outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:cursor-not-allowed disabled:opacity-50 md:text-ui-base"
        onClick={isMobile ? () => changeOpen(!open) : undefined}
      >
        <span className="flex min-w-0 items-center gap-1.5">
          <ProviderIcon provider={selectedProvider} className="shrink-0" />
          <span className="min-w-0 truncate">{label}</span>
        </span>
        <Icons.chevronDown aria-hidden className="size-4 shrink-0 opacity-50" />
      </button>
    );

  // On desktop the search input owns focus (it keeps it while arrowing), so
  // Radix's own focus-the-container-on-open is redirected to it. The sheet
  // leaves focus alone: a focused input would pop the keyboard over the list.
  const focusInput = (e: Event) => {
    e.preventDefault();
    // Touch keeps it unfocused; a fine pointer (desktop-class sheet) can type straight away.
    if (!isMobile || hasFinePointer()) {
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
            className="gap-0 p-0 data-[side=bottom]:h-[80dvh]"
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
        side={variant === "field" ? "bottom" : "top"}
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
