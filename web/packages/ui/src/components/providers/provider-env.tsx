import { useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useSetProviderEnv } from "@/hooks/use-set-provider-env";
import type { ProviderEnvEffective, ProviderInfo } from "@/lib/types";

/** The one launch env key surfaced per provider, with the field's label. */
const ENV_FIELDS: Record<string, { key: string; label: string }> = {
  "claude-native": { key: "CLAUDE_CONFIG_DIR", label: "Claude config directory" },
  "codex-native": { key: "CODEX_HOME", label: "Codex home" },
};

export const SOURCE_TEXT: Record<ProviderEnvEffective["source"], string> = {
  settings: "set in smind",
  environment: "from the daemon's environment",
  default: "CLI default",
};

/** Mirrors the daemon's rule: absolute, `~/`-relative, or a Windows drive path. */
function isAbsolutePath(value: string): boolean {
  return value.startsWith("/") || value.startsWith("~/") || /^[A-Za-z]:[\\/]/.test(value);
}

/**
 * Settings -> Providers: the directory a run's CLI keeps its config in (CLAUDE_CONFIG_DIR / CODEX_HOME), stored
 * as provider launch env. Rendered only for a provider whose `envEffective` carries the key; values come from `provider.list`.
 */
export function ProviderEnvField({ info, hasClient }: { info: ProviderInfo; hasClient: boolean }): JSX.Element | null {
  const field = ENV_FIELDS[info.id];
  const effective = field ? info.envEffective?.[field.key] : undefined;
  const configured = field ? info.env?.[field.key] : undefined;
  if (!field || !effective) return null;
  return <EnvFieldBody info={info} hasClient={hasClient} envKey={field.key} label={field.label} effective={effective} configured={configured} />;
}

function EnvFieldBody({
  info,
  hasClient,
  envKey,
  label,
  effective,
  configured,
}: {
  info: ProviderInfo;
  hasClient: boolean;
  envKey: string;
  label: string;
  effective: ProviderEnvEffective;
  configured: string | undefined;
}): JSX.Element {
  const { setEnv, pendingId, error } = useSetProviderEnv();
  const [draft, setDraft] = useState(configured ?? "");
  const [seen, setSeen] = useState(configured);
  const [invalid, setInvalid] = useState(false);
  // Reset the draft when the configured value changes underneath (a save landing, another window).
  if (seen !== configured) {
    setSeen(configured);
    setDraft(configured ?? "");
    setInvalid(false);
  }
  const pending = pendingId !== null;
  const trimmed = draft.trim();
  const save = (): void => {
    if (!isAbsolutePath(trimmed)) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setEnv(info.id, envKey, trimmed);
  };
  const id = `provider-env-input-${info.id}`;
  return (
    <div className="flex flex-col gap-1" data-testid={`provider-env-${info.id}`}>
      <label className="text-ui-sm font-medium" htmlFor={id}>
        {label}
      </label>
      <p className="text-ui-xs text-muted-foreground">Applies to the next run; a run already started keeps its setting.</p>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          value={draft}
          placeholder={effective.path}
          disabled={!hasClient || pending}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          data-testid={`provider-env-input-${info.id}`}
          onChange={(e) => {
            setDraft(e.target.value);
            setInvalid(false);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && hasClient && !pending && trimmed && trimmed !== (configured ?? "")) save();
          }}
        />
        <Button
          size="xs"
          disabled={!hasClient || pending || !trimmed || trimmed === (configured ?? "")}
          data-testid={`provider-env-save-${info.id}`}
          onClick={save}
        >
          {pending ? "Saving…" : "Save"}
        </Button>
        {configured !== undefined ? (
          <Button
            variant="ghost"
            size="xs"
            disabled={!hasClient || pending}
            data-testid={`provider-env-clear-${info.id}`}
            onClick={() => setEnv(info.id, envKey, null)}
          >
            Clear
          </Button>
        ) : null}
      </div>
      <p className="text-ui-xs text-muted-foreground" data-testid={`provider-env-using-${info.id}`}>
        Using <span className="font-mono">{effective.path}</span> · {SOURCE_TEXT[effective.source]}
      </p>
      {!effective.exists ? (
        <p className="text-ui-xs text-warning" data-testid={`provider-env-missing-${info.id}`}>
          This directory does not exist yet.
        </p>
      ) : null}
      {invalid ? (
        <p role="alert" className="text-ui-xs text-destructive">
          Enter an absolute path (or start with ~/)
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-ui-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
