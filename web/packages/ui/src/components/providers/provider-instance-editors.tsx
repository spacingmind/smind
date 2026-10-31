import { useState, type JSX } from "react";

import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { Switch } from "@/components/ui/switch";
import { useUpdateProviderInstance } from "@/hooks/use-provider-instances";
import { Icons } from "@/lib/icons";
import type { ProviderInfo, ProviderInstanceModel, UpdateProviderInstanceParams } from "@/lib/types";

/** The models editor's mode: which of the two overlay lists a save writes. */
type ModelsMode = "replace" | "add";

/** What every row of the group shares: the entry, one mutation, and the no-client disable. */
type EditorProps = {
  info: ProviderInfo;
  hasClient: boolean;
  pending: boolean;
  update: (params: UpdateProviderInstanceParams) => void;
  clearError: () => void;
  /** The refusal text, set only on the editor that raised it. */
  error: JSX.Element | null;
};

/** Which editor's save raised the shown error. */
type EditorName = "models" | "tools" | "inherit";

/** A row whose body is a full-width editor: the label and description take the whole row, the editor sits under them. */
const STACKED_ROW = "sm:flex-col sm:items-stretch sm:justify-start";

function sameIds(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id, i) => id === b[i]);
}

/**
 * Settings -> Providers: the editable overlay of one instance (`provider.updateInstance`): the model list
 * (replace the catalog or add to it), Claude Code's disallowed tools, and the ~/.claude inherit toggle.
 * Rendered only for a custom entry that extends another provider; the stored keys are edited under Keys.
 */
export function ProviderInstanceEditors({ info, hasClient }: { info: ProviderInfo; hasClient: boolean }): JSX.Element | null {
  const { update, pending, error, clearError } = useUpdateProviderInstance();
  const [failedIn, setFailedIn] = useState<EditorName | null>(null);
  if (!info.custom || !info.extends) return null;
  // The refusal renders under the row whose save raised it, so it sits next to the button that was pressed.
  const bind = (name: EditorName): EditorProps => ({
    info,
    hasClient,
    pending,
    update: (params) => {
      setFailedIn(name);
      update(params);
    },
    clearError: () => {
      setFailedIn(null);
      clearError();
    },
    error: failedIn === name ? <FormError message={error} testId="instance-editors-error" /> : null,
  });
  // A landed save re-reads provider.list, so a changed list re-initialises its own draft (remount via key);
  // each editor keys only on its own stored value so another row's save keeps an unsaved draft.
  const modelsKey = JSON.stringify([info.instance?.models ?? [], info.instance?.additionalModels ?? []]);
  const toolsKey = JSON.stringify(info.instance?.disallowedTools ?? []);
  return (
    <>
      <SettingsGroup
        testId={`provider-instance-editors-${info.id}`}
        title="Instance settings"
        description="Saved to config.yaml. Keys are edited under Keys."
      >
        <ModelsEditor key={modelsKey} {...bind("models")} />
        {info.harness === "claude-code" ? (
          <>
            <ToolsEditor key={toolsKey} {...bind("tools")} />
            <InheritRow {...bind("inherit")} />
          </>
        ) : null}
      </SettingsGroup>
    </>
  );
}

function ModelsEditor({ info, hasClient, pending, update, clearError, error }: EditorProps): JSX.Element {
  const storedModels = info.instance?.models ?? [];
  const storedAdditional = info.instance?.additionalModels ?? [];
  const storedMode: ModelsMode = storedAdditional.length > 0 ? "add" : "replace";
  const [mode, setMode] = useState<ModelsMode>(storedMode);
  const [ids, setIds] = useState<string[]>((storedMode === "add" ? storedAdditional : storedModels).map((m) => m.id));
  const [typed, setTyped] = useState("");
  // The daemon keeps an untouched id's label/description/default; only a newly typed id is sent bare.
  const stored = new Map([...storedModels, ...storedAdditional].map((m) => [m.id, m] as const));
  const disabled = !hasClient || pending;
  // The save writes the chosen list and empties the other, so it is a no-op only when both match what is stored.
  const unchanged =
    sameIds(mode === "replace" ? ids : [], storedModels.map((m) => m.id)) &&
    sameIds(mode === "add" ? ids : [], storedAdditional.map((m) => m.id));
  const add = (): void => {
    const id = typed.trim();
    if (!id || ids.includes(id)) return;
    clearError();
    setIds([...ids, id]);
    setTyped("");
  };
  const save = (): void => {
    const list: ProviderInstanceModel[] = ids.map((id) => stored.get(id) ?? { id });
    update(
      mode === "replace"
        ? { id: info.id, models: list, additionalModels: [] }
        : { id: info.id, models: [], additionalModels: list },
    );
  };
  const reset = (): void => {
    clearError();
    setMode(storedMode);
    setIds((storedMode === "add" ? storedAdditional : storedModels).map((m) => m.id));
    setTyped("");
  };
  return (
    <SettingsRow
      testId={`instance-models-${info.id}`}
      label="Models"
      description="Replace list swaps the provider's catalog; Add to list extends it."
      className={STACKED_ROW}
    >
      <div className="flex flex-col gap-2">
        <SegmentedControl<ModelsMode>
          label="Models mode"
          testId="instance-models-mode"
          disabled={disabled}
          value={mode}
          onChange={(next) => {
            clearError();
            setMode(next);
          }}
          options={[
            { value: "replace", label: "Replace list", testId: "instance-mode-replace" },
            { value: "add", label: "Add to list", testId: "instance-mode-add" },
          ]}
        />
        {ids.length === 0 ? (
          <p className="text-ui-sm text-muted-foreground" data-testid="instance-models-empty">
            No models set — the provider's own catalog is used.
          </p>
        ) : (
          <ul className="flex flex-col gap-1" data-testid="instance-models-list">
            {ids.map((id) => (
              <li
                key={id}
                className="flex items-center justify-between gap-2 rounded-lg border border-border px-2 py-1"
                data-testid={`instance-model-row-${id}`}
              >
                <span className="truncate font-mono text-ui-sm">{id}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Remove model ${id}`}
                  data-testid={`instance-model-remove-${id}`}
                  disabled={disabled}
                  onClick={() => {
                    clearError();
                    setIds(ids.filter((x) => x !== id));
                  }}
                >
                  <Icons.close aria-hidden />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex items-center gap-2">
          <Input
            aria-label="Model id"
            data-testid="instance-model-input"
            value={typed}
            disabled={disabled}
            spellCheck={false}
            placeholder="model id"
            onChange={(e) => {
              clearError();
              setTyped(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="xs"
            data-testid="instance-model-add"
            disabled={disabled || !typed.trim()}
            onClick={add}
          >
            <Icons.add aria-hidden />
            Add
          </Button>
        </div>
        <div className="flex justify-end gap-2">
          {unchanged ? null : (
            <Button type="button" variant="ghost" size="xs" disabled={disabled} data-testid="instance-models-reset" onClick={reset}>
              Reset
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={disabled || unchanged}
            data-testid="instance-models-save"
            onClick={save}
          >
            Save models
          </Button>
        </div>
        {error}
      </div>
    </SettingsRow>
  );
}

function ToolsEditor({ info, hasClient, pending, update, clearError, error }: EditorProps): JSX.Element {
  const stored = info.instance?.disallowedTools ?? [];
  const [tools, setTools] = useState<string[]>(stored);
  const [typed, setTyped] = useState("");
  const disabled = !hasClient || pending;
  const unchanged = sameIds(tools, stored);
  const add = (): void => {
    const name = typed.trim();
    if (!name || tools.includes(name)) return;
    clearError();
    setTools([...tools, name]);
    setTyped("");
  };
  return (
    <SettingsRow
      testId={`instance-tools-${info.id}`}
      label="Disallowed tools"
      description="Claude Code tools this instance may not call."
      className={STACKED_ROW}
    >
      <div className="flex flex-col gap-2">
        {tools.length === 0 ? (
          <p className="text-ui-sm text-muted-foreground" data-testid="instance-tools-empty">
            Every tool is allowed.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5" data-testid="instance-tools-list">
            {tools.map((name) => (
              <span
                key={name}
                className="inline-flex items-center gap-1 rounded-full border border-border px-1.5 py-0.5 text-ui-sm text-muted-foreground"
                data-testid={`instance-tool-${name}`}
              >
                <span className="font-mono">{name}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="size-4"
                  aria-label={`Remove tool ${name}`}
                  data-testid={`instance-tool-remove-${name}`}
                  disabled={disabled}
                  onClick={() => {
                    clearError();
                    setTools(tools.filter((t) => t !== name));
                  }}
                >
                  <Icons.close aria-hidden />
                </Button>
              </span>
            ))}
          </div>
        )}
        <div className="flex items-center gap-2">
          <Input
            aria-label="Tool name"
            data-testid="instance-tool-input"
            value={typed}
            disabled={disabled}
            spellCheck={false}
            placeholder="e.g. WebSearch"
            onChange={(e) => {
              clearError();
              setTyped(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="xs"
            data-testid="instance-tool-add"
            disabled={disabled || !typed.trim()}
            onClick={add}
          >
            <Icons.add aria-hidden />
            Add
          </Button>
        </div>
        <div className="flex justify-end gap-2">
          {unchanged ? null : (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              disabled={disabled}
              data-testid="instance-tools-reset"
              onClick={() => {
                clearError();
                setTools(stored);
                setTyped("");
              }}
            >
              Reset
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={disabled || unchanged}
            data-testid="instance-tools-save"
            onClick={() => update({ id: info.id, disallowedTools: tools })}
          >
            Save
          </Button>
        </div>
        {error}
      </div>
    </SettingsRow>
  );
}

function InheritRow({ info, hasClient, pending, update, clearError, error }: EditorProps): JSX.Element {
  return (
    <SettingsRow
      testId={`instance-inherit-${info.id}`}
      label="Use ~/.claude"
      description="Share your default Claude Code login and settings instead of a private config directory."
      control={
        <Switch
          aria-label="Use ~/.claude"
          data-testid="instance-inherit-switch"
          checked={info.instance?.inheritClaudeConfig === true}
          disabled={!hasClient || pending}
          onCheckedChange={(next) => {
            clearError();
            update({ id: info.id, inheritClaudeConfig: next });
          }}
        />
      }
      className="sm:flex-wrap"
    >
      {error}
    </SettingsRow>
  );
}
