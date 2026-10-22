// providerCatalog.ts is M6's pure selection logic over provider.list's
// catalog: what a new-task form starts with, how model/mode follow the
// chosen provider, and what actually goes on the wire (unchanged model/mode
// are omitted so the daemon default applies). Kept pure -- no react-native
// import -- so vitest covers it against plain data (the followUpPrompt
// discipline). Mirrors Paseo's provider-selection/preferences shape.

import { ModeInfo, ModelInfo, ProviderInfo } from './api';

export interface NewTaskSelection {
  provider: string;
  model: string; // '' = "daemon default"
  mode: string; // '' = "daemon default"
}

/** The last selection, per provider (session-only, newTaskPrefs owns storing it). */
export interface RememberedSelection {
  provider?: string;
  byProvider?: Record<string, { model?: string; mode?: string }>;
}

/** The provider's remembered entry, else nothing. */
function rememberedFor(remembered: RememberedSelection | undefined, providerId: string): { model?: string; mode?: string } {
  return remembered?.byProvider?.[providerId] ?? {};
}

/** The remembered model if the provider still lists it, else the catalog's default-flagged model, else ''. */
function resolveModel(p: ProviderInfo, remembered: RememberedSelection | undefined): string {
  const r = rememberedFor(remembered, p.id).model;
  if (r && p.models?.some((m) => m.id === r)) return r;
  return p.models?.find((m) => m.default)?.id ?? '';
}

/** The remembered mode if the provider still lists it, else defaultMode, else ''. */
function resolveMode(p: ProviderInfo, remembered: RememberedSelection | undefined): string {
  const r = rememberedFor(remembered, p.id).mode;
  if (r && p.modes?.some((m) => m.id === r)) return r;
  return p.defaultMode ?? '';
}

/**
 * The selection a form starts with: the remembered provider if it is still
 * in the catalog, else the first provider; model/mode per that provider
 * (remembered else defaults). null for an empty catalog.
 */
export function defaultSelection(providers: ProviderInfo[], remembered?: RememberedSelection): NewTaskSelection | null {
  if (providers.length === 0) return null;
  const id = remembered?.provider && providers.some((p) => p.id === remembered.provider) ? remembered.provider : providers[0].id;
  return selectProvider(providers, id, remembered);
}

/** Switching provider re-resolves model+mode for that provider (remembered else defaults). */
export function selectProvider(providers: ProviderInfo[], providerId: string, remembered?: RememberedSelection): NewTaskSelection {
  const p = providers.find((x) => x.id === providerId);
  if (!p) return { provider: providerId, model: '', mode: '' };
  return { provider: p.id, model: resolveModel(p, remembered), mode: resolveMode(p, remembered) };
}

/** label ?? id. */
export function providerLabel(p: ProviderInfo): string {
  return p.label ?? p.id;
}

/** First option is the synthetic Default ('' = daemon default), then the catalog's models. */
export function modelOptions(p: ProviderInfo | undefined): Array<{ value: string; label: string }> {
  return [{ value: '', label: 'Default' }, ...(p?.models ?? []).map((m: ModelInfo) => ({ value: m.id, label: m.label }))];
}

/** The catalog's modes; auto-approves ones say so. No synthetic Default: defaultMode is preselected. */
export function modeOptions(p: ProviderInfo | undefined): Array<{ value: string; label: string }> {
  return (p?.modes ?? []).map((m: ModeInfo) => ({ value: m.id, label: m.autoApproves ? `${m.label} (auto-approves)` : m.label }));
}

/** What goes on the wire: '' and the daemon's default mode are omitted (undefined), so the default applies. */
export function wireModelMode(p: ProviderInfo | undefined, sel: NewTaskSelection): { model?: string; permissionMode?: string } {
  const out: { model?: string; permissionMode?: string } = {};
  if (sel.model) out.model = sel.model;
  if (sel.mode && sel.mode !== p?.defaultMode) out.permissionMode = sel.mode;
  return out;
}
