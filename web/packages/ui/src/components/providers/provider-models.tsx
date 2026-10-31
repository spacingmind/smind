import { useMemo, useState, type JSX } from "react";

import type { ProviderInfo } from "@/lib/types";
import { filterAndRankModelRows, type ModelRow } from "@/lib/model-browser";
import { StatusBadge } from "@/components/ui/status-badge";
import { Input } from "@/components/ui/input";

const SEARCH_THRESHOLD = 8;

function toModelRows(models: NonNullable<ProviderInfo["models"]>): ModelRow[] {
  return models.map((model) => ({
    key: `${model.id}`,
    provider: "",
    providerLabel: "",
    modelId: model.id,
    modelLabel: model.label,
    description: model.description ?? model.id,
    isDefault: model.default,
  }));
}

/** The catalog's source: an instance shows where its own overlay's list comes from, not the extended one. */
function sourceNote(provider: ProviderInfo): string {
  if (provider.recipe) return "From the recipe";
  if (provider.modelsDiscovered) return "Reported by the agent";
  if (provider.custom && !provider.extends) return "Listed in config.yaml";
  if (provider.extends) {
    if ((provider.instance?.models ?? []).length > 0) return "Listed in config.yaml";
    if ((provider.instance?.additionalModels ?? []).length > 0) return "Built-in catalog + config.yaml";
  }
  return "Built-in catalog";
}

/** Read-only model catalog for a provider: source note, search over long lists, no selection. */
export function ProviderModels(props: { provider: ProviderInfo }): JSX.Element {
  const { provider } = props;
  const [query, setQuery] = useState("");
  const models = useMemo(() => {
    const all = provider.models ?? [];
    // A recipe instance lists only its own configured models; the catalog entries come from the runner it extends.
    return provider.recipe ? all.filter((m) => m.source === "config") : all;
  }, [provider.models, provider.recipe]);
  const rows = useMemo(
    () => filterAndRankModelRows(toModelRows(models), query.trim().toLowerCase()),
    [models, query],
  );

  if (models.length === 0) {
    return (
      <div className="rounded-lg border bg-card p-3" data-testid="provider-models">
        <p className="text-ui-sm text-muted-foreground" data-testid="provider-models-empty">
          {provider.recipe
            ? "No models listed for this recipe — runs use the endpoint's default model."
            : "No models reported yet — the agent hasn't been probed."}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-card p-3" data-testid="provider-models">
      <div className="flex items-center gap-2">
        <h4 className="text-ui-base font-medium">Models</h4>
        <span className="text-ui-sm text-muted-foreground">{models.length}</span>
        <span className="text-ui-sm text-muted-foreground">{sourceNote(provider)}</span>
      </div>
      {models.length >= SEARCH_THRESHOLD ? (
        <Input
          aria-label="Search models"
          data-testid="models-search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-7 w-44"
        />
      ) : null}
      <ul className="flex flex-col">
        {rows.map((row) => (
          <li key={row.modelId} className="flex flex-col gap-0.5 border-t py-1.5 first:border-t-0">
            <span className="flex items-baseline gap-2">
              <span className="text-ui-base">{row.modelLabel}</span>
              {row.isDefault ? (
                <StatusBadge
                  status="success"
                  className="border-border bg-muted text-muted-foreground"
                >
                  Default
                </StatusBadge>
              ) : null}
              <span className="min-w-0 flex-1 truncate text-right font-mono text-ui-xs text-muted-foreground">
                {row.modelId}
              </span>
            </span>
            {row.description && row.description !== row.modelId ? (
              <span className="text-ui-xs text-muted-foreground">{row.description}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
