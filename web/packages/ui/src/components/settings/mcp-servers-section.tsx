import { useEffect, useState, type FormEvent } from "react";
import { MoreHorizontal, Plug, Plus } from "lucide-react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import type { McpServer } from "@/lib/types";

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
}

interface McpFormState {
  name: string;
  transport: "stdio" | "http" | "sse";
  command: string;
  args: string;
  env: McpKVRow[];
  url: string;
  headers: McpKVRow[];
}

const EMPTY_FORM: McpFormState = { name: "", transport: "stdio", command: "", args: "", env: [], url: "", headers: [] };


/** Mirrors the daemon's own validation (internal/mcpservers.normalizeValidate) so obvious mistakes never leave the form: name always required, command for stdio, url for http/sse, and every *newly added* env/headers key needs a real value (an existing key may hold the "[redacted]" placeholder). */
function validateForm(form: McpFormState): string | null {
  if (!form.name.trim()) return "Name is required.";
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
    env: Object.entries(m.env).map(([key, value]) => ({ key, value })),
    url: m.url,
    headers: Object.entries(m.headers).map(([key, value]) => ({ key, value })),
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
 * edit form renders existing values as masked password inputs holding the
 * placeholder, and sending the placeholder back means "keep the stored
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

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<McpServer[]>("mcp.list")
      .then((list) => {
        if (!cancelled) setServers(list ?? []);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
    return () => {
      cancelled = true;
    };
  }, [client]);

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
    return () => {
      offCreated();
      offUpdated();
      offDeleted();
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
    if (!client) return;
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
    try {
      await client.call("mcp.delete", { id });
      setServers((prev) => (prev ? prev.filter((s) => s.id !== id) : prev));
      if (openCard === id) closeCard();
    } catch (err) {
      console.error("mcp.delete failed", err);
    } finally {
      setDeleting(null);
    }
  }

  const newServerButton = (
    <Button type="button" size="sm" data-testid="mcp-new-button" onClick={openNew}>
      <Plus aria-hidden className="size-3.5" /> New server
    </Button>
  );

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-mcp-servers">
      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-ui-base font-medium text-foreground">MCP servers</h3>
          {newServerButton}
        </div>

        {servers === null ? (
          <p className="text-ui-base text-muted-foreground">Loading…</p>
        ) : (
          <>
            {servers.length === 0 && openCard !== "new" && (
              <EmptyState
                testId="mcp-empty-state"
                title="No MCP servers yet"
                description="Give agents tools like a browser (Playwright) — add an MCP server"
                action={newServerButton}
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
                          </span>
                          <span className="truncate text-ui-sm text-muted-foreground">{mcpSummaryLine(m)}</span>
                        </div>
                        <label
                          className="flex shrink-0 cursor-pointer items-center gap-1.5 text-ui-sm text-foreground-muted"
                          title={m.enabled ? "Disable this server" : "Enable this server"}
                        >
                          <input
                            type="checkbox"
                            data-testid={`mcp-enabled-${m.id}`}
                            checked={m.enabled}
                            aria-label={`${m.name} enabled`}
                            onChange={(e) => void handleToggleEnabled(m, e.target.checked)}
                          />
                          {m.enabled ? "Enabled" : "Disabled"}
                        </label>
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
                              <MoreHorizontal aria-hidden />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem
                              data-testid={`mcp-edit-${m.id}`}
                              onSelect={() => (editing ? closeCard() : startEdit(m))}
                            >
                              {editing ? "Close" : "Edit"}
                            </DropdownMenuItem>
                            {deleting === m.id ? (
                              <DropdownMenuItem
                                data-testid={`mcp-delete-confirm-${m.id}`}
                                className="text-destructive"
                                onSelect={() => void handleDelete(m.id)}
                              >
                                Really delete {m.name}?
                              </DropdownMenuItem>
                            ) : (
                              <DropdownMenuItem
                                data-testid={`mcp-delete-${m.id}`}
                                onSelect={() => setDeleting(m.id)}
                              >
                                Delete
                              </DropdownMenuItem>
                            )}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                      {editing && (
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
          <p role="alert" data-testid="mcp-error" className="text-ui-base text-destructive">
            {error}
          </p>
        )}
      </section>
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
 * input; leaving the placeholder untouched sends it back verbatim, which
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
        <input
          aria-label="Server name"
          data-testid={`${idPrefix}-name`}
          placeholder="Name (e.g. playwright)"
          value={form.name}
          onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
        />
        <select
          aria-label="Transport"
          data-testid={`${idPrefix}-transport`}
          value={form.transport}
          onChange={(e) => setForm((f) => ({ ...f, transport: e.target.value as McpFormState["transport"] }))}
          className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused"
        >
          <option value="stdio">stdio</option>
          <option value="http">http</option>
          <option value="sse">sse</option>
        </select>
      </div>

      {form.transport === "stdio" ? (
        <>
          <input
            aria-label="Command"
            data-testid={`${idPrefix}-command`}
            placeholder="Command (e.g. npx)"
            value={form.command}
            onChange={(e) => setForm((f) => ({ ...f, command: e.target.value }))}
            className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
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
          <input
            aria-label="URL"
            data-testid={`${idPrefix}-url`}
            placeholder="URL (e.g. https://example.com/mcp)"
            value={form.url}
            onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
            className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
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
        <p role="alert" data-testid={`${idPrefix}-error`} className="text-ui-base text-destructive">
          {error}
        </p>
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
      {rows.map((row, i) => (
        <div key={i} className="flex items-center gap-1" data-testid={`${idPrefix}-row-${i}`}>
          <input
            aria-label={`${label} key ${i + 1}`}
            data-testid={`${idPrefix}-key-${i}`}
            placeholder="Key"
            value={row.key}
            onChange={(e) => onChange(rows.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
            className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
          />
          <input
            type="password"
            aria-label={`${label} value ${i + 1}`}
            data-testid={`${idPrefix}-value-${i}`}
            placeholder="Value"
            value={row.value}
            onChange={(e) => onChange(rows.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
            className="h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
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
      ))}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-testid={`${idPrefix}-add`}
        onClick={() => onChange([...rows, { key: "", value: "" }])}
      >
        <Plus aria-hidden className="size-3.5" /> Add
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
  icon: <Plug aria-hidden className="size-3.5" />,
  render: (ctx) => <McpServersSection {...ctx} />,
});
