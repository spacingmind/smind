import { useState, type JSX } from "react";

import { SettingsEmptyCard, SettingsGroup } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge } from "@/components/ui/status-badge";
import { useAddFromRecipe, useProviderRecipes } from "@/hooks/use-provider-recipes";
import { Icons } from "@/lib/icons";
import type { ProviderRecipe } from "@/lib/types";

const EMPTY_FORM = { id: "", label: "", baseUrl: "", key: "", modelInput: "" };

/**
 * Settings -> Providers -> Add from recipe: instantiate one row of the daemon's `provider.recipes` catalog as a
 * providers.custom entry. The pasted API key is write-only: it lives in the input's controlled state and one local
 * const inside the submit handler, is blanked the moment the call is made (whatever the outcome), and never reaches
 * a ref, the query cache, a log line or the rendered text; the daemon answers with a fingerprint only.
 */
export function AddFromRecipe({ hasClient, onClose }: { hasClient: boolean; onClose: () => void }): JSX.Element {
  const { recipes, loaded, error } = useProviderRecipes(true);
  const { add, pending, error: addError, clearError } = useAddFromRecipe();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [models, setModels] = useState<string[]>([]);
  const selected = recipes.find((r) => r.id === selectedId) ?? null;

  const pick = (recipe: ProviderRecipe): void => {
    clearError();
    setSelectedId(recipe.id);
    setForm(EMPTY_FORM);
    setModels(recipe.models.map((m) => m.id));
  };
  const back = (): void => {
    clearError();
    setSelectedId(null);
    setForm(EMPTY_FORM);
    setModels([]);
  };
  const edit = (patch: Partial<typeof EMPTY_FORM>): void => setForm((f) => ({ ...f, ...patch }));
  const trimmedModelInput = form.modelInput.trim();
  const canAddModel = trimmedModelInput !== "" && !models.includes(trimmedModelInput);
  const addModel = (): void => {
    if (!canAddModel) return;
    setModels((prev) => [...prev, trimmedModelInput]);
    edit({ modelInput: "" });
  };
  const keyMissing = selected?.needsKey && form.key.trim() === "";
  const modelsMissing = selected?.modelsRequired && models.length === 0;
  const baseUrlMissing = selected?.needsBaseUrl && form.baseUrl.trim() === "";
  const submitDisabled = !hasClient || pending || !!keyMissing || !!modelsMissing || !!baseUrlMissing;

  return (
    <SettingsGroup title="Add from recipe" testId="provider-add-recipe">
      {error ? (
        <FormError message={error} size="sm" testId="provider-recipes-error" />
      ) : !loaded ? (
        <LoadingRows className="border-0 bg-transparent p-0" />
      ) : !selected ? (
        recipes.length === 0 ? (
          <SettingsEmptyCard title="No recipes" testId="provider-recipes-empty" />
        ) : (
          recipes.map((r) => (
            <button
              key={r.id}
              type="button"
              aria-label={r.label}
              data-testid={`recipe-card-${r.id}`}
              disabled={!hasClient}
              onClick={() => pick(r)}
              className="flex w-full items-center gap-3 px-4 py-3 text-left outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
            >
              <Icons.recipe aria-hidden className="size-4 shrink-0 text-foreground-muted" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-ui-base text-foreground">{r.label}</span>
                <span className="block truncate text-ui-sm text-foreground-muted">{r.description}</span>
              </span>
              <StatusBadge status="neutral" className="hidden sm:inline-flex">
                {r.endpointKind}
              </StatusBadge>
              {r.unverified ? (
                <StatusBadge status="warning" data-testid={`recipe-unverified-${r.id}`}>
                  Unverified
                </StatusBadge>
              ) : null}
              <Icons.chevronRight aria-hidden className="size-4 shrink-0 text-foreground-subtle" />
            </button>
          ))
        )
      ) : (
        <form
          data-testid="recipe-form"
          className="flex flex-col gap-3 px-4 py-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (submitDisabled) return;
            // The key is read into this const and the field is blanked before the call: whatever the daemon answers,
            // the value never sits in the UI (or anywhere else) afterwards.
            const key = form.key;
            edit({ key: "" });
            add(
              {
                recipe: selected.id,
                id: form.id.trim() || undefined,
                label: form.label.trim() || undefined,
                baseUrl: selected.needsBaseUrl ? form.baseUrl.trim() : undefined,
                models,
                secrets: selected.needsKey ? { "api-key": key } : undefined,
              },
              () => {
                back();
                onClose();
              },
            );
          }}
        >
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" size="icon-sm" aria-label="Back" data-testid="recipe-back" onClick={back}>
              <Icons.back aria-hidden className="size-4" />
            </Button>
            <span className="text-ui-base font-medium text-foreground">{selected.label}</span>
          </div>
          {selected.notes.length > 0 ? (
            <ul data-testid="recipe-notes" className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
              {selected.notes.map((note) => (
                <li key={note}>{note}</li>
              ))}
            </ul>
          ) : null}
          {selected.unverified ? (
            <p data-testid="recipe-unverified-note" className="text-ui-sm text-warning">
              Not verified against a live endpoint yet{selected.unverifiedAspects.length > 0 ? ` (${selected.unverifiedAspects.join(", ")})` : ""}.
            </p>
          ) : null}
          {selected.docs.startsWith("https://") ? (
            <a
              href={selected.docs}
              target="_blank"
              rel="noreferrer noopener"
              data-testid="recipe-docs"
              className="inline-flex w-fit items-center gap-1 text-ui-sm text-primary outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/50"
            >
              Docs <Icons.externalLink aria-hidden className="size-3" />
            </a>
          ) : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="flex flex-col gap-1 text-ui-sm font-medium">
              Id
              <Input
                value={form.id}
                placeholder={selected.id}
                data-testid="recipe-id"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                disabled={pending}
                onChange={(e) => edit({ id: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-ui-sm font-medium">
              Label
              <Input
                value={form.label}
                placeholder={selected.label}
                data-testid="recipe-label"
                disabled={pending}
                onChange={(e) => edit({ label: e.target.value })}
              />
            </label>
          </div>
          {selected.needsBaseUrl ? (
            <label className="flex flex-col gap-1 text-ui-sm font-medium">
              Base URL
              <Input
                value={form.baseUrl}
                placeholder="https://…"
                data-testid="recipe-base-url"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                disabled={pending}
                onChange={(e) => edit({ baseUrl: e.target.value })}
              />
            </label>
          ) : null}
          <div className="flex flex-col gap-1.5">
            <span className="text-ui-sm font-medium">Models</span>
            {models.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5" data-testid="recipe-models">
                {models.map((m) => (
                  <span
                    key={m}
                    data-testid={`recipe-model-${m}`}
                    className="inline-flex items-center gap-1 rounded-lg border border-input-border bg-input px-2 py-0.5 font-mono text-ui-sm text-foreground"
                  >
                    {m}
                    <button
                      type="button"
                      aria-label={`Remove ${m}`}
                      data-testid={`recipe-model-remove-${m}`}
                      onClick={() => setModels((prev) => prev.filter((x) => x !== m))}
                      className="inline-flex outline-none hover:text-foreground-muted focus-visible:ring-2 focus-visible:ring-ring/50"
                    >
                      <Icons.close aria-hidden className="size-3" />
                    </button>
                  </span>
                ))}
              </div>
            ) : null}
            {modelsMissing ? <FormError message="This recipe needs at least one model id." size="sm" testId="recipe-models-required" /> : null}
            <div className="flex items-center gap-1.5">
              <Input
                value={form.modelInput}
                placeholder="Add a model id"
                data-testid="recipe-model-input"
                className="w-56"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                aria-label="Add a model id"
                disabled={pending}
                onChange={(e) => edit({ modelInput: e.target.value })}
                onKeyDown={(e) => {
                  if (e.key !== "Enter") return;
                  e.preventDefault(); // Enter adds the model, it does not submit the form.
                  addModel();
                }}
              />
              <Button type="button" variant="outline" size="sm" data-testid="recipe-model-add" disabled={!canAddModel || pending} onClick={addModel}>
                Add
              </Button>
            </div>
          </div>
          {selected.needsKey ? (
            <label className="flex flex-col gap-1 text-ui-sm font-medium">
              API key
              <Input
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={form.key}
                data-testid="recipe-key"
                aria-label="API key"
                className="font-mono"
                disabled={pending}
                onChange={(e) => edit({ key: e.target.value })}
              />
              <span className="text-ui-xs text-foreground-muted">Stored by the daemon, never shown again.</span>
            </label>
          ) : null}
          <div className="flex flex-col gap-1">
            <Button type="submit" size="sm" data-testid="recipe-submit" disabled={submitDisabled}>
              {pending ? "Adding…" : "Add provider"}
            </Button>
            {addError ? (
              <>
                <FormError message={addError} size="sm" testId="recipe-error" />
                {selected.needsKey ? <p className="text-ui-sm text-foreground-muted">Re-enter the key and try again.</p> : null}
              </>
            ) : null}
          </div>
        </form>
      )}
    </SettingsGroup>
  );
}
