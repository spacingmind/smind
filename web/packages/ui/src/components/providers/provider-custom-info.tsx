import type { JSX } from "react";

import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { SOURCE_TEXT } from "@/components/providers/provider-env";
import { useReloadProviders } from "@/hooks/use-provider-instances";
import type { ProviderInfo } from "@/lib/types";

/**
 * Settings -> Providers, detail of a `providers.custom` entry (an instance or an ACP provider): for an instance, the
 * config directory its runs use and where that came from (the same line as the built-ins); for any custom entry, how
 * to change or remove it. The config file is the only editor (`provider.setEnv`/`setDefaultModel` are for the four
 * built-ins), so the note says so and offers `provider.reload` once the file was edited.
 */
export function ProviderCustomInfo({ info, hasClient }: { info: ProviderInfo; hasClient: boolean }): JSX.Element {
  const { reload, pending, error } = useReloadProviders();
  const dirs = Object.entries(info.envEffective ?? {});
  return (
    <div className="flex flex-col gap-2" data-testid={`provider-custom-${info.id}`}>
      {dirs.map(([key, effective]) => (
        <div key={key} className="flex flex-col gap-0.5" data-testid={`provider-custom-dir-${info.id}`}>
          <span className="text-ui-sm font-medium">{key === "CODEX_HOME" ? "Codex home" : "Config directory"}</span>
          <p className="text-ui-xs text-muted-foreground">
            Using <span className="font-mono">{effective.path}</span> · {SOURCE_TEXT[effective.source]}
          </p>
          {!effective.exists ? (
            <p className="text-ui-xs text-warning" data-testid={`provider-custom-missing-${info.id}`}>
              This directory does not exist yet.
            </p>
          ) : null}
        </div>
      ))}
      <div className="flex items-center justify-between gap-2">
        <p className="text-ui-xs text-muted-foreground">
          Defined under <span className="font-mono">providers.custom</span> in config.yaml. To change or remove it, edit that entry, then reload.
        </p>
        <Button
          variant="outline"
          size="xs"
          disabled={!hasClient || pending}
          data-testid={`provider-reload-${info.id}`}
          onClick={reload}
          className="shrink-0"
        >
          {pending ? "Reloading…" : "Reload providers"}
        </Button>
      </div>
      <FormError message={error} size="sm" testId={`provider-reload-error-${info.id}`} />
    </div>
  );
}
