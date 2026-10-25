import { useState, type JSX } from "react";

import { ProviderIcon } from "@/components/provider-icon";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { useAddProviderInstance, useProviderDirSuggestions } from "@/hooks/use-provider-instances";
import { Icons } from "@/lib/icons";

type Base = "claude-native" | "codex-native";

const BASE_OPTIONS: { value: Base; label: string }[] = [
  { value: "claude-native", label: "Claude Code" },
  { value: "codex-native", label: "Codex" },
];

const EMPTY_FORM = { base: "claude-native" as Base, id: "", label: "", dir: "" };

/**
 * Settings -> Providers -> Add instance (ADR-0042 AC37): another login of Claude Code or Codex in its own config
 * directory, side by side with the base provider. Directories the daemon found in the home folder are one click
 * (`provider.addInstance` with the suggested id/label/dir); anything else goes through the small form. The daemon's
 * refusal shows under the control that caused it. The new instance appears through `provider.updated`.
 */
export function AddProviderInstance({ hasClient }: { hasClient: boolean }): JSX.Element {
  const { suggestions } = useProviderDirSuggestions(true);
  const { add, pending, error, clearError } = useAddProviderInstance();
  const [manual, setManual] = useState(false);
  const [form, setForm] = useState(EMPTY_FORM);
  // The error belongs to the control last used, so a chip's refusal is not repeated under the form (and vice versa).
  const [errorFrom, setErrorFrom] = useState<"chip" | "form">("chip");
  const formReady = form.id.trim() !== "" && form.label.trim() !== "" && form.dir.trim() !== "";
  const edit = (patch: Partial<typeof EMPTY_FORM>): void => {
    clearError();
    setForm((f) => ({ ...f, ...patch }));
  };
  return (
    <div data-testid="provider-add-instance" className="@container/add flex flex-col gap-3 rounded-xl border bg-card p-3">
      <div className="flex flex-col gap-0.5">
        <h3 className="text-ui-base font-medium">Add instance</h3>
        <p className="text-ui-sm text-muted-foreground">
          A second Claude Code or Codex login in its own config directory, usable side by side with the base provider.
        </p>
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-ui-sm font-medium">Found in your home folder</span>
        <div className="flex min-h-7 flex-wrap items-center gap-1.5" data-testid="provider-add-suggestions">
          {suggestions.length === 0 ? (
            <span className="text-ui-sm text-muted-foreground">No other ~/.claude* or ~/.codex* directories found.</span>
          ) : (
            suggestions.map((s) => (
              <button
                key={s.dir}
                type="button"
                disabled={!hasClient || pending}
                data-testid={`provider-suggestion-${s.suggestedId}`}
                title={s.dir}
                onClick={() => {
                  setErrorFrom("chip");
                  add({ extends: s.extends, id: s.suggestedId, label: s.suggestedLabel, dir: s.dir });
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-input-border bg-input px-2 text-ui-sm outline-none transition-colors hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
              >
                <ProviderIcon provider={s.extends} size={14} />
                <span className="font-mono">~/{s.name}</span>
                <Icons.arrowRight aria-hidden className="size-3 text-muted-foreground" />
                <span>{s.suggestedLabel}</span>
              </button>
            ))
          )}
        </div>
        {errorFrom === "chip" ? <FormError message={error} size="sm" testId="provider-add-error" /> : null}
      </div>

      <div className="flex flex-col gap-2">
        <button
          type="button"
          aria-expanded={manual}
          data-testid="provider-add-manual-toggle"
          onClick={() => setManual((v) => !v)}
          className="flex w-fit items-center gap-1 text-ui-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          {manual ? <Icons.chevronDown aria-hidden className="size-3" /> : <Icons.chevronRight aria-hidden className="size-3" />}
          Another directory
        </button>
        {manual ? (
          <form
            data-testid="provider-add-form"
            className="flex flex-col gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!hasClient || pending || !formReady) return;
              setErrorFrom("form");
              add(
                { extends: form.base, id: form.id.trim(), label: form.label.trim(), dir: form.dir.trim() },
                () => setForm(EMPTY_FORM),
              );
            }}
          >
            <SegmentedControl label="Extends" value={form.base} options={BASE_OPTIONS} onChange={(base) => edit({ base })} disabled={pending} />
            <div className="grid gap-2 @md/add:grid-cols-2">
              <label className="flex flex-col gap-1 text-ui-sm font-medium">
                Id
                <Input
                  value={form.id}
                  placeholder="claude-work"
                  data-testid="provider-add-id"
                  spellCheck={false}
                  autoCapitalize="off"
                  autoCorrect="off"
                  disabled={pending}
                  onChange={(e) => edit({ id: e.target.value })}
                />
              </label>
              <label className="flex flex-col gap-1 text-ui-sm font-medium">
                Label
                <Input value={form.label} placeholder="Claude work" data-testid="provider-add-label" disabled={pending} onChange={(e) => edit({ label: e.target.value })} />
              </label>
            </div>
            <label className="flex flex-col gap-1 text-ui-sm font-medium">
              Config directory
              <Input
                value={form.dir}
                placeholder="~/.claude-work"
                data-testid="provider-add-dir"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                disabled={pending}
                onChange={(e) => edit({ dir: e.target.value })}
              />
            </label>
            <div className="flex items-center gap-2">
              <Button type="submit" size="xs" disabled={!hasClient || pending || !formReady} data-testid="provider-add-submit">
                {pending && errorFrom === "form" ? "Adding…" : "Add instance"}
              </Button>
            </div>
            {errorFrom === "form" ? <FormError message={error} size="sm" testId="provider-add-error" /> : null}
          </form>
        ) : null}
      </div>
    </div>
  );
}
