import { useState, type KeyboardEvent } from "react";

import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { BROWSER_ALLOW_MAX } from "@/lib/browser-settings-types";
import { Icons } from "@/lib/icons";

interface BrowserAllowlistEditorProps {
  hosts: string[];
  /** Resolves when the daemon accepted the add; rejects with an Error whose message is the daemon's text. The editor never validates the host itself. */
  onAdd: (host: string) => Promise<void>;
  onRemove: (host: string) => Promise<void>;
  /** The daemon's rule for names, shown under the input. */
  note?: string;
  max?: number;
  disabled?: boolean;
}

/**
 * The Settings -> Browser allowlist editor (the spec's "The allowlist editor"): exact hosts only,
 * a controlled, daemon-free component so the section owns the RPCs and this is testable without one.
 * Validation is the daemon's: `onAdd`/`onRemove` return its verdict, a rejection's message is shown
 * verbatim and the typed host is kept (with `aria-invalid`) until it is corrected.
 */
export function BrowserAllowlistEditor({
  hosts,
  onAdd,
  onRemove,
  note,
  max = BROWSER_ALLOW_MAX,
  disabled = false,
}: BrowserAllowlistEditorProps) {
  const [draft, setDraft] = useState("");
  // One error slot: where it renders depends on which control was refused
  // (the add under the input, the remove under the list), so the test id is
  // never ambiguous.
  const [error, setError] = useState<{ message: string; place: "add" | "remove" } | null>(null);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);

  const trimmed = draft.trim();
  const full = hosts.length >= max;

  async function submit() {
    if (disabled || adding || removing !== null || !trimmed || full) return;
    setAdding(true);
    setError(null);
    try {
      await onAdd(trimmed);
      setDraft("");
    } catch (err) {
      setError({ message: err instanceof Error ? err.message : String(err), place: "add" });
    } finally {
      setAdding(false);
    }
  }

  async function remove(host: string) {
    if (disabled || removing !== null) return;
    setRemoving(host);
    setError(null);
    try {
      await onRemove(host);
    } catch (err) {
      setError({ message: err instanceof Error ? err.message : String(err), place: "remove" });
    } finally {
      setRemoving(null);
    }
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") {
      e.preventDefault();
      void submit();
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-2 sm:flex-row" aria-busy={adding || undefined}>
        <Input
          aria-label="Host to allow"
          aria-invalid={error?.place === "add" || undefined}
          data-testid="browser-allow-input"
          type="text"
          autoComplete="off"
          spellCheck={false}
          placeholder="intranet.example or 192.0.2.10"
          value={draft}
          disabled={disabled || adding || full}
          onChange={(e) => {
            setDraft(e.target.value);
            setError(null);
          }}
          onKeyDown={onKeyDown}
        />
        <Button
          variant="outline"
          size="sm"
          type="button"
          data-testid="browser-allow-add"
          disabled={disabled || adding || full || !trimmed}
          onClick={() => void submit()}
        >
          Add
        </Button>
      </div>
      {error?.place === "add" && <FormError message={error.message} testId="browser-allow-error" />}
      {note && <p className="text-ui-sm text-foreground-muted">{note}</p>}
      {full && (
        <p data-testid="browser-allow-full" className="text-ui-sm text-foreground-muted">
          The list is full ({max} hosts).
        </p>
      )}
      {hosts.length === 0 ? (
        <p data-testid="browser-allow-empty" className="text-ui-sm text-foreground-muted">
          Only localhost is reachable.
        </p>
      ) : (
        <ul aria-label="Allowed hosts" data-testid="browser-allow-list" className="flex flex-col gap-1">
          {hosts.map((host) => (
            <li
              key={host}
              className="flex items-center justify-between gap-3"
              aria-busy={removing === host || undefined}
            >
              <span className="min-w-0 break-all font-mono text-ui-sm">{host}</span>
              <Button
                variant="ghost"
                size="sm"
                type="button"
                className="shrink-0"
                aria-label={`Remove ${host}`}
                aria-busy={removing === host || undefined}
                disabled={disabled || removing === host}
                onClick={() => void remove(host)}
              >
                <Icons.delete aria-hidden />
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      {error?.place === "remove" && <FormError message={error.message} testId="browser-allow-error" />}
      <p data-testid="browser-allow-count" className="text-ui-sm text-foreground-muted">
        {hosts.length} of {max} hosts
      </p>
    </div>
  );
}
