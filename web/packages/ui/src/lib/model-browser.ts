// Pure (no React) port of Paseo's model-browser logic
// (model-browser-view.ts + provider-selection.ts), adapted to smind's
// ProviderInfo/ModelInfo wire types for the combined provider+model picker.

import type { ProviderInfo } from "@/lib/types";

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

function buildModelRowKey(provider: string, modelId: string): string {
  return `${provider}:${modelId}`;
}

function buildSyntheticDefaultRow(provider: string, providerLabel: string): ModelRow {
  return {
    key: buildModelRowKey(provider, ""),
    provider,
    providerLabel,
    modelId: "",
    modelLabel: DEFAULT_MODEL_LABEL,
    isDefault: true,
  };
}

export function buildBrowserProviders(
  providers: ProviderInfo[],
  health: Record<string, ProviderHealth | undefined>,
): BrowserProvider[] {
  return providers.map((info) => {
    const label = info.label ?? info.id;
    const rows: ModelRow[] = [buildSyntheticDefaultRow(info.id, label)];
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

function isWordBoundaryChar(ch: string): boolean {
  return !/[a-z0-9]/.test(ch);
}

function scoreField(field: string, normalizedQuery: string): number {
  const idx = field.toLowerCase().indexOf(normalizedQuery);
  if (idx === 0) {
    return 300;
  }
  if (idx > 0) {
    return isWordBoundaryChar(field.toLowerCase()[idx - 1] ?? "") ? 200 : 100;
  }
  // In-order subsequence.
  let pos = 0;
  for (const ch of field.toLowerCase()) {
    if (ch === normalizedQuery[pos]) {
      pos += 1;
      if (pos === normalizedQuery.length) {
        return 10;
      }
    }
  }
  return 0;
}

function getModelRowSearchFields(row: ModelRow): string[] {
  return [row.modelLabel, row.modelId, row.providerLabel, row.description ?? ""];
}

function scoreModelRow(row: ModelRow, normalizedQuery: string): number {
  let best = 0;
  for (const field of getModelRowSearchFields(row)) {
    const score = scoreField(field, normalizedQuery);
    if (score > best) {
      best = score;
    }
  }
  return best;
}

export function filterAndRankModelRows(
  rows: ModelRow[],
  normalizedQuery: string,
): ModelRow[] {
  if (!normalizedQuery) {
    return rows;
  }
  const scored = rows
    .map((row, index) => ({ row, index, score: scoreModelRow(row, normalizedQuery) }))
    .filter((entry) => entry.score > 0);
  scored.sort((a, b) => b.score - a.score || a.index - b.index);
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

/** Where the picker lands when it opens. A sole provider skips the redundant root view. */
export function resolveInitialModelBrowserView({
  providers,
  selectedProvider,
  selectedModel,
  hasProfiles,
}: {
  providers: BrowserProvider[];
  selectedProvider: string;
  selectedModel: string;
  hasProfiles: boolean;
}): ModelBrowserView {
  const singleProvider = providers.length === 1 ? providers[0] : undefined;
  if (singleProvider) {
    return {
      kind: "provider",
      providerId: singleProvider.id,
      providerLabel: singleProvider.label,
    };
  }

  if (hasProfiles) {
    return { kind: "all" };
  }

  if (selectedProvider.length > 0 && selectedModel.length > 0) {
    const provider = providers.find((entry) => entry.id === selectedProvider);
    if (provider) {
      return { kind: "provider", providerId: provider.id, providerLabel: provider.label };
    }
  }

  return { kind: "all" };
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
  if (!selectedModel) {
    return provider.label;
  }
  const rows = getProviderModelRows(provider);
  const row = rows.find((entry) => entry.modelId === selectedModel);
  if (row) {
    return `${provider.label} · ${row.modelLabel}`;
  }
  if (selectedModel) {
    return `${provider.label} · ${selectedModel}`;
  }
  return provider.label;
}
