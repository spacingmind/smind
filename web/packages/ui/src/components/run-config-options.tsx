import { useState, type FormEvent } from "react";

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
 * The live ACP session's config-option row (GLM/Kimi's thinking level, and
 * whatever else a given agent advertises) -- see use-run-config-options'
 * doc comment for why this only ever exists once a session is live, and
 * task-detail.tsx for where it's mounted. Renders nothing when there's
 * nothing to show and no error to report, so an idle/non-ACP run leaves no
 * trace in the layout.
 */
export function RunConfigOptions({
  options,
  error,
  onSetOption,
}: {
  options: ConfigOptionParams[];
  error: string | null;
  onSetOption: (configId: string, value: string) => Promise<void>;
}) {
  if (options.length === 0 && !error) return null;

  return (
    <div
      data-testid="run-config-options"
      className="mx-auto flex w-full max-w-3xl shrink-0 flex-wrap items-center gap-2 px-4 py-1.5"
    >
      {options.map((option) => (
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
        variant="outline"
        size="xs"
        disabled={pending}
        aria-label={option.name}
        aria-pressed={on}
        title={option.description}
        data-testid="run-config-option-toggle"
        onClick={() => change(on ? "false" : "true")}
      >
        {option.name}: {on ? "On" : "Off"}
      </Button>
    );
  }

  // A "select" option the agent advertised real named choices for (e.g.
  // GLM's thinking-level tiers) -- a real dropdown of the agent's own
  // labels, read from the RPC response, never hardcoded here.
  if (option.options && option.options.length > 0) {
    return (
      <Select value={current} onValueChange={change} disabled={pending}>
        <SelectTrigger
          aria-label={option.name}
          title={option.description}
          className="h-7 w-auto gap-1 border-0 bg-transparent px-2 text-ui-sm hover:bg-hover"
        >
          <SelectValue placeholder={option.name} />
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
