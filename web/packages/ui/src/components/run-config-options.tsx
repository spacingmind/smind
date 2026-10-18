import { useState, type FormEvent } from "react";

import { Icons } from "@/lib/icons";

import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import { cn } from "@/lib/utils";
import { ThinkingSelect } from "@/components/pickers/thinking-select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { ConfigOptionParams } from "@/lib/types";

/** ConfigOptionParams.currentValue as displayable text -- raw JSON per internal/acp.ConfigOption's doc comment (a bare string for "select", a bare boolean for "boolean"). */
function currentValueText(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return String(value);
  return "";
}

/**
 * The live ACP session's config-option controls (GLM/Kimi's thinking
 * level, and whatever else a given agent advertises) -- see
 * use-run-config-options' doc comment for why this only ever exists once
 * a session is live. Renders inline inside the composer's RunConfigToolbar
 * row (run-config IA plan: these used to be a separate full-width bar
 * above the composer; the AC now wants them "in the same row, not a
 * separate control elsewhere"), so this renders no outer width/padding of
 * its own -- the toolbar row already provides that. Selects and toggles use
 * the toolbar's pill trigger look (D2 of live-run-composer-and-noise).
 * `omitMode` drops the agent's own `mode`-category option while the
 * toolbar's live Mode pill already drives that same session mode. Renders nothing when
 * there's nothing to show and no error to report, so an idle/non-ACP run
 * leaves no trace in the layout.
 */
export function RunConfigOptions({
  options,
  error,
  onSetOption,
  omitMode = false,
}: {
  options: ConfigOptionParams[];
  error: string | null;
  onSetOption: (configId: string, value: string) => Promise<void>;
  /** True while the toolbar's live Mode pill is active: the `mode`-category option is that same control. */
  omitMode?: boolean;
}) {
  const shown = omitMode ? options.filter((option) => option.category !== "mode") : options;
  if (shown.length === 0 && !error) return null;

  return (
    <div data-testid="run-config-options" className="flex flex-wrap items-center">
      {shown.map((option) => (
        <ConfigOptionControl key={option.configId} option={option} onSetOption={onSetOption} />
      ))}
      {error && (
        <span data-testid="run-config-options-error" role="alert" className="text-ui-sm text-destructive">
          {error}
        </span>
      )}
    </div>
  );
}

function ConfigOptionControl({
  option,
  onSetOption,
}: {
  option: ConfigOptionParams;
  onSetOption: (configId: string, value: string) => Promise<void>;
}) {
  const [pending, setPending] = useState(false);
  const current = currentValueText(option.currentValue);

  const change = async (value: string) => {
    setPending(true);
    try {
      await onSetOption(option.configId, value);
    } catch {
      // Surfaced via the shared error slot above (use-run-config-options
      // already recorded it) -- nothing further to do here.
    } finally {
      setPending(false);
    }
  };

  // A boolean option is a two-state toggle -- no enumerated choices to
  // pick from, so a Select would be a dropdown with exactly one useful
  // click in it. A plain toggle button says the same thing more directly.
  if (option.type === "boolean") {
    const on = current === "true";
    return (
      <Button
        type="button"
        variant="ghost"
        disabled={pending}
        aria-label={option.name}
        aria-pressed={on}
        title={option.description}
        data-testid="run-config-option-toggle"
        className={cn(PILL_TRIGGER_CLASS, on && "text-foreground")}
        onClick={() => change(on ? "false" : "true")}
      >
        <Icons.options aria-hidden className="shrink-0" />
        <span className="max-sm:hidden">
          {option.name}: {on ? "On" : "Off"}
        </span>
      </Button>
    );
  }

  // A "select" option the agent advertised real named choices for (e.g.
  // GLM's thinking-level tiers) -- a real dropdown of the agent's own
  // labels, read from the RPC response, never hardcoded here.
  if (option.options && option.options.length > 0) {
    // ADR-0025: the live `model` option (Claude's synthetic one or an ACP
    // agent's own) switches the running session, unlike the composer's
    // Model picker, which sets the next run's -- so it is named apart from
    // it, both for sighted users (tooltip) and assistive tech/tests (label).
    const isModel = option.category === "model";
    if (!isModel) {
      // GLM/Kimi's thinking tiers and any other agent-advertised select: the toolbar's shared pill select.
      return (
        <ThinkingSelect
          variant="pill"
          label={option.name}
          title={option.description}
          levels={option.options.map((choice) => ({ id: choice.value, label: choice.name, help: choice.description }))}
          value={current}
          onChange={change}
          disabled={pending}
        />
      );
    }
    return (
      <Select value={current} onValueChange={change} disabled={pending}>
        <SelectTrigger
          aria-label={`${option.name} (this session)`}
          title={option.description ?? "Switch the model for this running session"}
          data-testid="run-config-option-model"
          className={PILL_TRIGGER_CLASS}
        >
          <Icons.model aria-hidden className="shrink-0" />
          {/* Prefixed so it reads apart from the composer's next-run Model picker sitting beside it. */}
          <SelectValue placeholder={option.name}>
            This run: {option.options.find((c) => c.value === current)?.name ?? current}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {option.options.map((choice) => (
            <SelectItem key={choice.value} value={choice.value} title={choice.description}>
              {choice.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  }

  // No enumerated choices to build a dropdown from -- an option kind this
  // package doesn't otherwise model, or a select the agent advertised with
  // no choices list. A raw text field is the honest fallback: it still
  // lets the option be changed, without fabricating a list of values this
  // package was never told about.
  return (
    <ConfigOptionTextField
      configId={option.configId}
      name={option.name}
      description={option.description}
      defaultValue={current}
      pending={pending}
      onSubmit={change}
    />
  );
}

function ConfigOptionTextField({
  configId,
  name,
  description,
  defaultValue,
  pending,
  onSubmit,
}: {
  configId: string;
  name: string;
  description?: string;
  defaultValue: string;
  pending: boolean;
  onSubmit: (value: string) => void;
}) {
  const [value, setValue] = useState(defaultValue);
  const inputId = `config-option-${configId}`;

  function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    onSubmit(value);
  }

  return (
    <form className="flex items-center gap-1" onSubmit={handleSubmit}>
      <label htmlFor={inputId} className="text-ui-sm text-foreground-muted">
        {name}
      </label>
      <Input
        id={inputId}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        disabled={pending}
        title={description}
        className="h-7 w-28 text-ui-sm"
      />
      <Button type="submit" variant="outline" size="xs" disabled={pending}>
        Set
      </Button>
    </form>
  );
}
