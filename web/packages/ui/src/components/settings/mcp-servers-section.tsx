import { useEffect, useState, type FormEvent } from "react";

import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingRows } from "@/components/ui/loading-rows";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useLastDefined } from "@/hooks/use-last-defined";
import { FormError } from "@/components/ui/form-error";

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { McpServer } from "@/lib/types";
import { Icons } from "@/lib/icons";
import { OwnerBadge, ownedTooltip } from "@/components/plugins/owner-badge";

/**
 * The add/edit form's field state -- one instance shared by whichever card
 * is open (the "new server" card or a row's inline edit), mirroring
 * ProfilesSection's own single-open-card model. Rows here are simple
 * {key, value} pairs so env/headers can add/remove entries inline; value
 * holds the literal "[redacted]" placeholder for an existing entry the
 * user hasn't retyped, which is what mcp.update expects to mean "keep the
 * stored secret".
 */
export interface McpKVRow {
  key: string;
  value: string;
  /** True for an entry loaded from the daemon: its real value is never known here, so `value` holds the "[redacted]" placeholder until the user types a replacement. */
  stored?: boolean;
}

/** What the daemon returns for every env/headers value, and what sending it back means ("keep the stored secret"). */
const REDACTED_PLACEHOLDER = "[redacted]";

interface McpFormState {
  name: string;
  transport: "stdio" | "http" | "sse";
  command: string;
  args: string;
  env: McpKVRow[];
  url: string;
  headers: McpKVRow[];
}

/** Same charset the daemon enforces on a server name (internal/mcpservers validMcpServerName). */
const VALID_SERVER_NAME = /^[A-Za-z0-9_-]+$/;

const EMPTY_FORM: McpFormState = { name: "", transport: "stdio", command: "", args: "", env: [], url: "", headers: [] };


/** Mirrors the daemon's own validation (internal/mcpservers.normalizeValidate) so obvious mistakes never leave the form: name always required, command for stdio, url for http/sse, and every *newly added* env/headers key needs a real value (an existing key may hold the "[redacted]" placeholder). */
function validateForm(form: McpFormState): string | null {
  const name = form.name.trim();
  if (!name) return "Name is required.";
  if (!VALID_SERVER_NAME.test(name)) return "Name may only contain letters, digits, '_' and '-'.";
  if (form.transport === "stdio" && !form.command.trim()) return "Command is required for stdio servers.";
  if (form.transport !== "stdio" && !form.url.trim()) return "URL is required for http/sse servers.";
  const kvError = (rows: McpKVRow[], what: string): string | null => {
    for (const r of rows) {
      const key = r.key.trim();
      if (!key && !r.value) continue;
      if (!key) return `${what}: every entry needs a key.`;
      if (!r.value) return `${what}: ${key} needs a value.`;
    }
    return null;
  };
  return kvError(form.env, "Environment variables") ?? kvError(form.headers, "Headers");
}

/** Which card is open: "new" for the add card, a server id for that row's inline edit, or null for none. */
type OpenCard = number | "new" | null;

function formFromServer(m: McpServer): McpFormState {
  return {
    name: m.name,
    transport: m.transport,
    command: m.command,
    args: m.args.join("\n"),
    env: Object.entries(m.env).map(([key, value]) => ({ key, value, stored: true })),
    url: m.url,
    headers: Object.entries(m.headers).map(([key, value]) => ({ key, value, stored: true })),
  };
}

/**
 * Settings -> MCP servers (ADR-0018): manage the daemon's MCP server
 * registry -- stdio commands and http/sse endpoints that give agents
 * extra tools. Same list/inline-card shape as ProfilesSection: a header
 * "+ New server" button, one form open at a time, live updates via the
 * mcpServer.* topics.
 *
 * Secrets: every env/headers value the daemon returns is the literal
 * "[redacted]" (keys preserved). The list never shows values at all; the
 * edit form renders existing values as empty masked password inputs
 * ("Stored — type to replace") whose underlying value stays the placeholder
 * until the user types, and sending the placeholder back means "keep the stored
 * secret" (the daemon handles that). A newly added key must carry a real
 * value -- see the form's own validation.
 */
function McpServersSection({ client, events }: SettingsSectionContext) {
  const [servers, setServers] = useState<McpServer[] | null>(null);
  const [openCard, setOpenCard] = useState<OpenCard>(null);
  const [form, setForm] = useState<McpFormState>(EMPTY_FORM);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<number | null>(null);
  const deleteAction = useConfirmAction(deleting !== null);
  const lastDeleting = useLastDefined(deleting);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadAttempt, setLoadAttempt] = useState(0);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<McpServer[]>("mcp.list")
      .then((list) => {
        if (cancelled) return;
        setLoadError(null);
        setServers(list ?? []);
      })
      .catch((err) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [client, loadAttempt]);

  useEffect(() => {
    if (!events) return;
    const upsert = (payload: unknown) => {
      const s = (payload as { server?: McpServer } | undefined)?.server;
      if (!s) return;
      setServers((prev) => (prev ? upsertServer(prev, s) : [s]));
    };
    const offCreated = events.subscribe("mcpServer.created", upsert);
    const offUpdated = events.subscribe("mcpServer.updated", upsert);
    const offDeleted = events.subscribe("mcpServer.deleted", (payload) => {
      const id = (payload as { id?: number } | undefined)?.id;
      if (id === undefined) return;
      setServers((prev) => (prev ? prev.filter((s) => s.id !== id) : prev));
    });
    // A plugin was enabled, disabled, reloaded or removed, or the global switch flipped: the rows it owns appear or
    // vanish (the daemon also publishes mcpServer.created/deleted for each), and one re-read settles the rest.
    const offPlugin = events.subscribe("plugin.updated", () => setLoadAttempt((n) => n + 1));
    return () => {
      offCreated();
      offUpdated();
      offDeleted();
      offPlugin();
    };
  }, [events]);

  function openNew() {
    setOpenCard("new");
    setForm(EMPTY_FORM);
    setError(null);
  }

  function startEdit(m: McpServer) {
    setOpenCard(m.id);
    setForm(formFromServer(m));
    setError(null);
  }

  function closeCard() {
    setOpenCard(null);
    setError(null);
  }

  async function handleToggleEnabled(m: McpServer, enabled: boolean) {
    // A plugin-owned row is read-only: the daemon would answer read_only_owned, so don't send it.
    if (!client || m.readOnly || m.owner) return;
    // Optimistic flip, reconciled by the mcpServer.updated event / result.
    setServers((prev) => (prev ? prev.map((s) => (s.id === m.id ? { ...s, enabled } : s)) : prev));
    try {
      const saved = await client.call<McpServer>("mcp.setEnabled", { id: m.id, enabled });
      setServers((prev) => (prev ? upsertServer(prev, saved) : [saved]));
    } catch (err) {
      setServers((prev) => (prev ? upsertServer(prev, m) : [m]));
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!client || openCard === null) return;
    const invalid = validateForm(form);
    if (invalid) {
      setError(invalid);
      return;
    }
    setPending(true);
    setError(null);
    try {
      const params = formToParams(form);
      const saved =
        openCard === "new"
          ? await client.call<McpServer>("mcp.create", params)
          : await client.call<McpServer>("mcp.update", { id: openCard, ...params });
      setServers((prev) => (prev ? upsertServer(prev, saved) : [saved]));
      closeCard();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  async function handleDelete(id: number) {
    if (!client) return;
    const done = await deleteAction.run(() => client.call("mcp.delete", { id }));
    if (!done) return;
    setServers((prev) => (prev ? prev.filter((s) => s.id !== id) : prev));
    if (openCard === id) closeCard();
    setDeleting(null);
  }

  const newServerButton = (
    <Button type="button" size="sm" data-testid="mcp-new-button" onClick={openNew}>
      <Icons.add aria-hidden className="size-3.5" /> New server
    </Button>
  );

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-mcp-servers">
      <section className="flex flex-col gap-2">
        <SettingsSectionTitle title="MCP servers" actions={newServerButton} />

        {servers === null ? (
          loadError ? (
            <div className="flex items-center gap-2">
              <Alert variant="error" testId="mcp-load-error" description={loadError} />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                data-testid="mcp-load-retry"
                onClick={() => {
                  setLoadError(null);
                  setLoadAttempt((n) => n + 1);
                }}
              >
                Retry
              </Button>
            </div>
          ) : (
            <LoadingRows />
          )
        ) : (
          <>
            {servers.length === 0 && openCard !== "new" && (
              <EmptyState
                testId="mcp-empty-state"
                title="No MCP servers yet"
                description="Give agents tools like a browser (Playwright) — add one with New server"
              />
            )}
            {(servers.length > 0 || openCard === "new") && (
              <ul className="flex flex-col gap-1 rounded-xl border bg-card p-3">
                {openCard === "new" && (
                  <li data-testid="mcp-new-form" className="rounded-lg px-2 py-1.5">
                    <McpServerForm
                      idPrefix="mcp-form"
                      form={form}
                      setForm={setForm}
                      onSubmit={handleSubmit}
                      onCancel={closeCard}
                      pending={pending}
                      error={error}
                      submitLabel={pending ? "Saving…" : "Add server"}
                    />
                  </li>
                )}
                {servers.map((m) => {
                  const editing = openCard === m.id;
                  // A plugin-owned row is read-only for every caller (the daemon answers `read_only_owned`): manage it in Settings -> Plugins.
                  const owned = m.readOnly === true || !!m.owner;
                  return (
                    <li
                      key={m.id}
                      data-testid={`mcp-row-${m.id}`}
                      className={`flex flex-col gap-2 rounded-lg px-2 py-1.5 hover:bg-hover ${m.enabled ? "" : "opacity-60"}`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex min-w-0 flex-1 flex-col">
                          <span className="flex items-center gap-2">
                            <span className="truncate text-ui-base font-medium text-foreground">{m.name}</span>
                            <span
                              data-testid={`mcp-transport-${m.id}`}
                              className="shrink-0 rounded-full border border-border px-1.5 text-ui-sm text-muted-foreground"
                            >
                              {m.transport}
                            </span>
                            <OwnerBadge owner={m.owner} testId={`mcp-owner-${m.id}`} />
                          </span>
                          <span className="truncate text-ui-sm text-muted-foreground">{mcpSummaryLine(m)}</span>
                        </div>
                        <label
                          className="flex shrink-0 cursor-pointer items-center gap-1.5 text-ui-sm text-foreground-muted"
                          title={m.owner ? ownedTooltip(m.owner) : m.enabled ? "Disable this server" : "Enable this server"}
                        >
                          <input
                            type="checkbox"
                            data-testid={`mcp-enabled-${m.id}`}
                            checked={m.enabled}
                            disabled={owned}
                            aria-label={`${m.name} enabled`}
                            onChange={(e) => void handleToggleEnabled(m, e.target.checked)}
                          />
                          {m.enabled ? "Enabled" : "Disabled"}
                        </label>
                        {!owned && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                aria-label={`${m.name} actions`}
                                data-testid={`mcp-menu-${m.id}`}
                                className="shrink-0"
                              >
                                <Icons.more aria-hidden />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem
                                data-testid={`mcp-edit-${m.id}`}
                                onSelect={() => (editing ? closeCard() : startEdit(m))}
                              >
                                {editing ? "Close" : "Edit"}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                data-testid={`mcp-delete-${m.id}`}
                                onSelect={() => {
                                  setDeleting(m.id);
                                  deleteAction.reset();
                                }}
                              >
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </div>
                      {editing && !owned && (
                        <McpServerForm
                          idPrefix={`mcp-edit-form-${m.id}`}
                          form={form}
                          setForm={setForm}
                          onSubmit={handleSubmit}
                          onCancel={closeCard}
                          pending={pending}
                          error={error}
                          submitLabel={pending ? "Saving…" : "Save"}
                        />
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
          </>
        )}
        {error && servers !== null && openCard === null && (
          <FormError message={error} testId="mcp-error" />
        )}
      </section>
      {(() => {
        const targetId = lastDeleting;
        const target = servers?.find((m) => m.id === targetId);
        return (
          <ConfirmDialog
            open={deleting !== null}
            onOpenChange={(open) => {
              if (open) return;
              setDeleting(null);
              deleteAction.reset();
            }}
            title="Delete MCP server?"
            description={
              <>
                Delete <span className="font-medium text-foreground">{target?.name}</span>? This removes its stored env/headers.
              </>
            }
            confirmLabel="Delete"
            pendingLabel="Deleting…"
            pending={deleteAction.pending}
            error={deleteAction.error}
            testId="mcp-delete-dialog"
            confirmTestId={`mcp-delete-confirm-${targetId}`}
            cancelTestId={`mcp-delete-cancel-${targetId}`}
            errorTestId={`mcp-delete-error-${targetId}`}
            onConfirm={() => deleting !== null && void handleDelete(deleting)}
          />
        );
      })()}
    </div>
  );
}

/**
 * The Name/Transport/Command-or-URL/Args/Env-or-Headers fields, reused by
 * the "New server" card and each row's inline edit (ProfilesSection's
 * shared-form pattern). Only the fields relevant to the selected transport
 * render -- stdio shows Command/Args/Env, http/sse show URL/Headers.
 *
 * Env/Headers render as key/value rows: existing entries' values arrive as
 * the literal "[redacted]" placeholder and render as a masked password
 * input that displays empty; leaving it untouched sends the placeholder back verbatim, which
 * the daemon interprets as "keep the stored secret". A newly added key
 * must have a real value.
 */
function McpServerForm({
  idPrefix,
  form,
  setForm,
  onSubmit,
  onCancel,
  pending,
  error,
  submitLabel,
}: {
  idPrefix: string;
  form: McpFormState;
  setForm: (updater: (f: McpFormState) => McpFormState) => void;
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
  pending: boolean;
  error: string | null;
  submitLabel: string;
}) {
  function fillPlaywright() {
    setForm((f) => ({
      ...f,
      name: f.name || "playwright",
      transport: "stdio",
      command: "npx",
      args: ["-y", "@playwright/mcp@latest", "--headless"].join("\n"),
    }));
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={onSubmit}>
      <div className="grid grid-cols-2 gap-2">
        <Input
          aria-label="Server name"
          data-testid={`${idPrefix}-name`}
          placeholder="Name (e.g. playwright)"
          value={form.name}
          onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
        />
        <Select
          value={form.transport}
          onValueChange={(value) => setForm((f) => ({ ...f, transport: value as McpFormState["transport"] }))}
        >
          <SelectTrigger aria-label="Transport" data-testid={`${idPrefix}-transport`} className="w-full">
            <SelectValue placeholder="Transport" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="stdio">stdio</SelectItem>
            <SelectItem value="http">http</SelectItem>
            <SelectItem value="sse">sse</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {form.transport === "stdio" ? (
        <>
          <Input
            aria-label="Command"
            data-testid={`${idPrefix}-command`}
            placeholder="Command (e.g. npx)"
            value={form.command}
            onChange={(e) => setForm((f) => ({ ...f, command: e.target.value }))}
          />
          <textarea
            aria-label="Arguments"
            data-testid={`${idPrefix}-args`}
            className="h-20 w-full resize-none rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
            placeholder="Arguments — one per line"
            value={form.args}
            onChange={(e) => setForm((f) => ({ ...f, args: e.target.value }))}
          />
          <KVEditor
            idPrefix={`${idPrefix}-env`}
            label="Environment variables"
            rows={form.env}
            onChange={(env) => setForm((f) => ({ ...f, env }))}
          />
          <p className="text-ui-sm text-foreground-subtlest">A bare command like npx is resolved on the daemon's PATH.</p>
        </>
      ) : (
        <>
          <Input
            aria-label="URL"
            data-testid={`${idPrefix}-url`}
            placeholder="URL (e.g. https://example.com/mcp)"
            value={form.url}
            onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
          />
          <KVEditor
            idPrefix={`${idPrefix}-headers`}
            label="Headers"
            rows={form.headers}
            onChange={(headers) => setForm((f) => ({ ...f, headers }))}
          />
        </>
      )}

      <div className="flex items-center gap-2">
        <Button type="button" variant="ghost" size="sm" data-testid={`${idPrefix}-playwright`} onClick={fillPlaywright}>
          Playwright (headless)
        </Button>
        <span className="text-ui-sm text-foreground-subtlest">First run may download a browser.</span>
      </div>

      {error && (
        <FormError message={error} testId={`${idPrefix}-error`} />
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" data-testid={`${idPrefix}-cancel`} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={pending} data-testid={`${idPrefix}-submit`}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

/** Key/value rows for env vars or headers: keys in the clear, values as password inputs (never shown). */
function KVEditor({
  idPrefix,
  label,
  rows,
  onChange,
}: {
  idPrefix: string;
  label: string;
  rows: McpKVRow[];
  onChange: (rows: McpKVRow[]) => void;
}) {
  return (
    <div className="flex flex-col gap-1" data-testid={idPrefix}>
      <span className="text-ui-sm text-foreground-muted">{label}</span>
      {rows.map((row, i) => {
        const kept = row.stored === true && row.value === REDACTED_PLACEHOLDER;
        return (
          <div key={i} className="flex items-center gap-1" data-testid={`${idPrefix}-row-${i}`}>
            <Input
              aria-label={`${label} key ${i + 1}`}
              data-testid={`${idPrefix}-key-${i}`}
              placeholder="Key"
              value={row.key}
              onChange={(e) => onChange(rows.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
            />
            <Input
              type="password"
              aria-label={`${label} value ${i + 1}`}
              data-testid={`${idPrefix}-value-${i}`}
              placeholder={kept ? "Stored — type to replace" : "Value"}
              value={kept ? "" : row.value}
              onChange={(e) => {
                // A stored entry shows empty rather than the literal
                // placeholder, so typing replaces the secret instead of
                // appending to "[redacted]"; clearing it again reverts to
                // "keep the stored secret".
                const value = e.target.value === "" && row.stored ? REDACTED_PLACEHOLDER : e.target.value;
                onChange(rows.map((r, j) => (j === i ? { ...r, value } : r)));
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={`Remove ${label.toLowerCase()} row ${i + 1}`}
              data-testid={`${idPrefix}-remove-${i}`}
              onClick={() => onChange(rows.filter((_, j) => j !== i))}
            >
              Remove
            </Button>
          </div>
        );
      })}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-testid={`${idPrefix}-add`}
        onClick={() => onChange([...rows, { key: "", value: "" }])}
      >
        <Icons.add aria-hidden className="size-3.5" /> Add
      </Button>
    </div>
  );
}

/** env/headers objects from the form's rows. "[redacted]" passes through verbatim -- the daemon treats it as "keep the stored secret". */
function formToParams(form: McpFormState): {
  name: string;
  transport: string;
  command: string;
  args: string[];
  env: Record<string, string>;
  url: string;
  headers: Record<string, string>;
} {
  const toObject = (rows: McpKVRow[]): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const r of rows) {
      const key = r.key.trim();
      if (key) out[key] = r.value;
    }
    return out;
  };
  return {
    name: form.name.trim(),
    transport: form.transport,
    command: form.transport === "stdio" ? form.command.trim() : "",
    args: form.transport === "stdio" ? form.args.split("\n").map((a) => a.trim()).filter(Boolean) : [],
    env: form.transport === "stdio" ? toObject(form.env) : {},
    url: form.transport === "http" || form.transport === "sse" ? form.url.trim() : "",
    headers: form.transport === "http" || form.transport === "sse" ? toObject(form.headers) : {},
  };
}

/** One-line summary: command + args for stdio, url for http/sse. Never includes env/header values. */
function mcpSummaryLine(m: McpServer): string {
  const parts: string[] = [];
  if (m.transport === "stdio") {
    parts.push([m.command, ...m.args].filter(Boolean).join(" "));
  } else if (m.url) {
    parts.push(m.url);
  }
  const envCount = Object.keys(m.env).length;
  if (envCount > 0) parts.push(`${envCount} env var${envCount === 1 ? "" : "s"}`);
  const headerCount = Object.keys(m.headers).length;
  if (headerCount > 0) parts.push(`${headerCount} header${headerCount === 1 ? "" : "s"}`);
  return parts.join(" · ");
}

function upsertServer(list: McpServer[], s: McpServer): McpServer[] {
  const index = list.findIndex((existing) => existing.id === s.id);
  if (index === -1) return [...list, s];
  const next = list.slice();
  next[index] = s;
  return next;
}

registerSettingsSection({
  id: "mcp-servers",
  label: "MCP servers",
  groupLabel: "Agents & providers",
  order: 177,
  icon: <Icons.mcp aria-hidden className="size-3.5" />,
  render: (ctx) => <McpServersSection {...ctx} />,
});
