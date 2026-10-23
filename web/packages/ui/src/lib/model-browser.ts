// Pure (no React) port of Paseo's model-browser logic
// (model-browser-view.ts + provider-selection.ts), adapted to smind's
// ProviderInfo/ModelInfo wire types for the combined provider+model picker.

import type { ProviderInfo } from "@/lib/types";
import { providerLabel } from "@/lib/provider-catalog";
import { compareMatchScores, scoreTextFields } from "./text-match";

export interface ModelRow {
  /** Row identity — `${provider}:${modelId}`. Empty modelId is the provider's synthetic default row. */
  key: string;
  provider: string;
  providerLabel: string;
  /** "" = the provider's default, send nothing. */
  modelId: string;
  modelLabel: string;
  description?: string;
  isDefault?: boolean;
  /** Synthetic default row only: the label of the configured default model (config providers.<id>.defaultModel) a run resolves to. */
  resolvedDefaultLabel?: string;
  /** Synthetic default row only: set when the owner named the row itself (`defaultRowLabel`), so the trigger shows that label. */
  customLabel?: boolean;
}

export type ProviderModelSelection =
  | { kind: "models"; rows: ModelRow[] }
  | { kind: "loading" }
  | { kind: "error"; message: string };

export interface BrowserProvider {
  id: string;
  label: string;
  modelSelection: ProviderModelSelection;
  /** Set when health says the provider cannot start a run; the reason shown next to the disabled row. */
  unavailableReason?: string;
}

export type ProviderHealth = { ok: boolean; detail: string };

export type ModelBrowserView =
  | { kind: "all" }
  | { kind: "provider"; providerId: string; providerLabel: string };

/** A profile's model reference; used to match profiles back to model rows. */
export interface ModelProfileRef {
  provider: string;
  modelId: string;
}

export const DEFAULT_MODEL_LABEL = "Default model";

// Desktop popover sizing, Paseo model-browser.tsx: the height follows the row
// count in the current view, clamped.
export const DESKTOP_PROVIDER_VIEW_MIN_HEIGHT = 220;
export const DESKTOP_PROVIDER_VIEW_MAX_HEIGHT = 400;
export const DESKTOP_PROVIDER_VIEW_BASE_HEIGHT = 80;
export const DESKTOP_MODEL_ROW_HEIGHT = 40;

function buildModelRowKey(provider: string, modelId: string): string {
  return `${provider}:${modelId}`;
}

function buildSyntheticDefaultRow(provider: string, providerLabel: string, resolved?: string, label?: string): ModelRow {
  const row: ModelRow = {
    key: buildModelRowKey(provider, ""),
    provider,
    providerLabel,
    modelId: "",
    modelLabel: label ?? DEFAULT_MODEL_LABEL,
    isDefault: true,
  };
  if (label !== undefined) row.customLabel = true;
  if (resolved) {
    row.resolvedDefaultLabel = resolved;
    row.description = `Currently ${resolved}`;
  }
  return row;
}

/** The label a configured default model id resolves to: the catalog's label when it lists the id, else the id itself. */
function resolveConfiguredDefault(info: ProviderInfo): string | undefined {
  if (!info.defaultModel) return undefined;
  return info.models?.find((m) => m.id === info.defaultModel)?.label ?? info.defaultModel;
}

export function buildBrowserProviders(
  providers: ProviderInfo[],
  health: Record<string, ProviderHealth | undefined>,
  options?: {
    loading?: boolean;
    /** What the synthetic "no model" row is called (default DEFAULT_MODEL_LABEL); Settings -> Providers names it "CLI default". */
    defaultRowLabel?: string;
  },
): BrowserProvider[] {
  return providers.map((info) => {
    const label = providerLabel(info.id, [info]);
    if (options?.loading) {
      const h = health[info.id];
      return h && h.ok === false
        ? { id: info.id, label, modelSelection: { kind: "loading" as const }, unavailableReason: h.detail || "Unavailable" }
        : { id: info.id, label, modelSelection: { kind: "loading" as const } };
    }
    const rows: ModelRow[] = [buildSyntheticDefaultRow(info.id, label, resolveConfiguredDefault(info), options?.defaultRowLabel)];
    for (const model of info.models ?? []) {
      rows.push({
        key: buildModelRowKey(info.id, model.id),
        provider: info.id,
        providerLabel: label,
        modelId: model.id,
        modelLabel: model.label,
        description: model.description ?? model.id,
        isDefault: model.default,
      });
    }
    const h = health[info.id];
    return h && h.ok === false
      ? { id: info.id, label, modelSelection: { kind: "models", rows }, unavailableReason: h.detail || "Unavailable" }
      : { id: info.id, label, modelSelection: { kind: "models", rows } };
  });
}

export function getProviderModelRows(p: BrowserProvider): ModelRow[] {
  return p.modelSelection.kind === "models" ? p.modelSelection.rows : [];
}

export function getAllModelRows(providers: BrowserProvider[]): ModelRow[] {
  return providers
    .filter((p) => !p.unavailableReason)
    .flatMap((p) => getProviderModelRows(p));
}

/** Rows a provider actually offers — the synthetic default row does not count (G15). */
export function getProviderModelCount(provider: BrowserProvider): number {
  return getProviderModelRows(provider).filter((row) => row.modelId !== "").length;
}

function getModelRowSearchFields(row: ModelRow): string[] {
  return [row.modelLabel, row.modelId, row.providerLabel, row.description ?? ""];
}

function scoreModelRow(row: ModelRow, normalizedQuery: string) {
  return scoreTextFields(normalizedQuery, getModelRowSearchFields(row));
}

export function filterAndRankModelRows(
  rows: ModelRow[],
  normalizedQuery: string,
): ModelRow[] {
  if (!normalizedQuery) {
    return rows;
  }
  const scored = rows
    .map((row) => ({ row, score: scoreModelRow(row, normalizedQuery) }))
    .filter((entry): entry is { row: ModelRow; score: NonNullable<typeof entry.score> } =>
      Boolean(entry.score),
    );

  scored.sort((a, b) => {
    const cmp = compareMatchScores(a.score, b.score);
    if (cmp !== 0) return cmp;
    return a.row.modelLabel.localeCompare(b.row.modelLabel);
  });

  return scored.map((entry) => entry.row);
}

/** What the root view shows: the provider drill-down, or ranked cross-provider results. */
export type ModelBrowserAllView =
  | { kind: "browse" }
  | { kind: "searchResults"; rows: ModelRow[] }
  | { kind: "noSearchMatches" };

export function resolveModelBrowserAllView({
  providers,
  normalizedQuery,
  isSearchFocused,
}: {
  providers: BrowserProvider[];
  normalizedQuery: string;
  isSearchFocused: boolean;
}): ModelBrowserAllView {
  if (!normalizedQuery && !isSearchFocused) {
    return { kind: "browse" };
  }
  const allRows = getAllModelRows(providers);
  const rows = normalizedQuery ? filterAndRankModelRows(allRows, normalizedQuery) : allRows;
  if (rows.length === 0) {
    return { kind: "noSearchMatches" };
  }
  return { kind: "searchResults", rows };
}

/**
 * Where the picker lands when it opens. A sole provider skips the redundant
 * root view; otherwise the selected provider's view wins — even for the
 * default model ("", ADR-0025) and when profiles exist (deviation from Paseo's
 * hasProfiles rule, see the plan's G1). `selectedModel` and `hasProfiles` are
 * accepted (and ignored) so existing callers compile.
 */
export function resolveInitialModelBrowserView({
  providers,
  selectedProvider,
}: {
  providers: BrowserProvider[];
  selectedProvider: string;
  /** Accepted (and ignored) so existing callers compile; see the doc comment. */
  selectedModel?: string;
  hasProfiles?: boolean;
}): ModelBrowserView {
  const singleProvider = providers.length === 1 ? providers[0] : undefined;
  if (singleProvider) {
    return {
      kind: "provider",
      providerId: singleProvider.id,
      providerLabel: singleProvider.label,
    };
  }

  if (selectedProvider.length > 0) {
    const provider = providers.find((entry) => entry.id === selectedProvider);
    if (provider) {
      return { kind: "provider", providerId: provider.id, providerLabel: provider.label };
    }
  }

  return { kind: "all" };
}

function countModelsInView(view: ModelBrowserView, providers: BrowserProvider[]): number {
  if (view.kind === "all") {
    return getAllModelRows(providers).length;
  }
  const provider = providers.find((entry) => entry.id === view.providerId);
  return provider?.modelSelection.kind === "models" ? getProviderModelRows(provider).length : 0;
}

/** Desktop popover height follows the view's row count, clamped (Paseo G7); `extraRows` counts non-model rows (profiles, headings). */
export function resolveDesktopFixedHeight(
  view: ModelBrowserView,
  providers: BrowserProvider[],
  extraRows = 0,
): number {
  const modelCount = countModelsInView(view, providers) + extraRows;
  return Math.min(
    Math.max(
      DESKTOP_PROVIDER_VIEW_MIN_HEIGHT,
      DESKTOP_PROVIDER_VIEW_BASE_HEIGHT + modelCount * DESKTOP_MODEL_ROW_HEIGHT,
    ),
    DESKTOP_PROVIDER_VIEW_MAX_HEIGHT,
  );
}

/**
 * Groups profiles by `provider:modelId`, skipping profiles that name no model.
 */
export function groupProfilesByProviderModel<T extends ModelProfileRef>(
  refs: readonly T[],
): Map<string, T[]> {
  const lookup = new Map<string, T[]>();
  for (const ref of refs) {
    const modelId = ref.modelId.trim();
    if (!modelId) {
      continue;
    }
    const key = `${ref.provider}:${modelId}`;
    const existing = lookup.get(key);
    if (existing) {
      existing.push(ref);
    } else {
      lookup.set(key, [ref]);
    }
  }
  return lookup;
}

/**
 * Trigger label is the model label only (Paseo buildSelectedTriggerLabel + G11)
 * — the UI shows a provider glyph beside it.
 */
export function resolveTriggerLabel({
  providers,
  selectedProvider,
  selectedModel,
}: {
  providers: BrowserProvider[];
  selectedProvider: string;
  selectedModel: string;
}): string {
  const provider = providers.find((entry) => entry.id === selectedProvider);
  if (!provider) {
    return "Select model";
  }
  if (provider.modelSelection.kind === "loading") {
    return "Loading…";
  }
  if (provider.modelSelection.kind === "error") {
    return "Error";
  }
  const rows = getProviderModelRows(provider);
  if (!selectedModel) {
    // An owner that named the "no model" row shows that name (a catalog model flagged default is not what is selected).
    const named = rows.find((row) => row.modelId === "" && row.customLabel);
    if (named) return named.modelLabel;
    const defaultRow = rows.find((row) => row.isDefault && row.modelId !== "");
    if (defaultRow) return defaultRow.modelLabel;
    // The synthetic default row: when a default is configured, show what a run resolves to.
    return rows.find((row) => row.modelId === "")?.resolvedDefaultLabel ?? provider.label;
  }
  const row = rows.find((entry) => entry.modelId === selectedModel);
  return row?.modelLabel ?? selectedModel;
}

/** Trigger tooltip/aria text: provider and model, de-duplicated when they coincide. */
export function resolveTriggerTitle({
  providers,
  selectedProvider,
  selectedModel,
}: {
  providers: BrowserProvider[];
  selectedProvider: string;
  selectedModel: string;
}): string {
  const label = resolveTriggerLabel({ providers, selectedProvider, selectedModel });
  if (label === "Select model" || label === "Loading…" || label === "Error") {
    return label;
  }
  const provider = providers.find((entry) => entry.id === selectedProvider);
  if (!provider) {
    return label;
  }
  if (label === provider.label) {
    return provider.label;
  }
  return `${provider.label} · ${label}`;
}
