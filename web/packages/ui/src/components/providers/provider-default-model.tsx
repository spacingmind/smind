import { useMemo, type JSX } from "react";

import { ProviderModelPicker } from "@/components/pickers/provider-model-picker";
import { useSetProviderDefaultModel } from "@/hooks/use-set-provider-default-model";
import { requestClearCooldown } from "@/lib/gate-events";
import { buildBrowserProviders } from "@/lib/model-browser";
import type { ProviderInfo } from "@/lib/types";

/** The name of the picker's "no model" row here: a run that names no model lets the CLI choose (or uses the configured default). */
const CLI_DEFAULT_LABEL = "CLI default";

/**
 * Settings -> Providers: the model a run that names none resolves to (config `providers.<id>.defaultModel`, U15).
 * The composer's own picker, field variant, bound to this provider; the value always comes from `provider.list`.
 */
export function ProviderDefaultModel({ info, hasClient }: { info: ProviderInfo; hasClient: boolean }): JSX.Element {
  const { setDefaultModel, pendingId, error } = useSetProviderDefaultModel();
  // The synthetic row must not carry a "Currently <model>" hint here: the row IS the choice, the line below says what it resolves to.
  const providers = useMemo(
    () => buildBrowserProviders([{ ...info, defaultModel: undefined }], {}, { defaultRowLabel: CLI_DEFAULT_LABEL }),
    [info],
  );
  const configured = info.defaultModel ?? "";
  const resolvedLabel = configured ? (info.models?.find((m) => m.id === configured)?.label ?? configured) : null;
  return (
    <div className="flex flex-col gap-1" data-testid={`provider-default-model-${info.id}`}>
      <span className="text-ui-sm font-medium" id={`provider-default-model-label-${info.id}`}>
        Default model
      </span>
      <ProviderModelPicker
        variant="field"
        testId={`provider-default-model-picker-${info.id}`}
        providers={providers}
        selectedProvider={info.id}
        selectedModel={configured}
        boundProvider={info.id}
        boundNotice={null}
        disabled={!hasClient || pendingId !== null}
        gatesBlock={false}
        onClearCooldown={requestClearCooldown}
        onSelect={(_provider, model) => setDefaultModel(info.id, model)}
      />
      <p className="text-ui-xs text-muted-foreground" data-testid="provider-default-resolves">
        {resolvedLabel ? `Runs use ${resolvedLabel}` : "Runs use the CLI's own default"}
      </p>
      {error ? (
        <p role="alert" className="text-ui-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
