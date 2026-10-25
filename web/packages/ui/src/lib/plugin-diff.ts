import type { PluginAcpEntry, PluginConsent, PluginMcpEntry } from "@/lib/plugin-types";

/** One line of what a pending update changes in what a plugin declares (ADR-0042 AC7). */
export interface PluginChange {
  kind: "added" | "removed" | "changed";
  /** What kind of thing: Version, Process, Capability, MCP server, ACP provider, Hook, Agent, Skill. */
  what: string;
  text: string;
  /** True for a change that grants or runs something more (the reason an update waits for consent). */
  runs: boolean;
}

/** How an MCP server is launched, for display: its command line, or its URL. */
export function describeMcp(s: PluginMcpEntry): string {
  const target = s.transport === "stdio" ? (s.command ?? []).join(" ") : (s.url ?? "");
  return `${s.transport} ${target}`.trim();
}

function byKey<T>(items: T[] | undefined, key: (t: T) => string): Map<string, T> {
  return new Map((items ?? []).map((t) => [key(t), t]));
}

function diffKeyed<T>(what: string, from: T[] | undefined, to: T[] | undefined, key: (t: T) => string, show: (t: T) => string, runs: boolean): PluginChange[] {
  const a = byKey(from, key);
  const b = byKey(to, key);
  const out: PluginChange[] = [];
  for (const [k, v] of b) {
    const before = a.get(k);
    if (!before) out.push({ kind: "added", what, text: show(v), runs });
    else if (show(before) !== show(v)) out.push({ kind: "changed", what, text: `${show(before)} → ${show(v)}`, runs });
  }
  for (const [k, v] of a) if (!b.has(k)) out.push({ kind: "removed", what, text: show(v), runs: false });
  return out;
}

const acp = (e: PluginAcpEntry) => `${e.id}: ${e.command.join(" ")}`;

/** What `next` declares that `current` does not (and the reverse): the diff an Apply asks the Human to accept. */
export function diffConsent(current: PluginConsent, next: PluginConsent): PluginChange[] {
  const out: PluginChange[] = [];
  if (current.version !== next.version) out.push({ kind: "changed", what: "Version", text: `${current.version} → ${next.version}`, runs: false });
  const proc = (c: PluginConsent) => (c.process ?? []).join(" ");
  if (proc(current) !== proc(next)) {
    out.push({ kind: proc(current) === "" ? "added" : proc(next) === "" ? "removed" : "changed", what: "Process", text: proc(current) && proc(next) ? `${proc(current)} → ${proc(next)}` : proc(next) || proc(current), runs: proc(next) !== "" });
  }
  out.push(...diffKeyed("Capability", current.capabilities, next.capabilities, (c) => c, (c) => c, true));
  out.push(...diffKeyed("MCP server", current.mcpServers, next.mcpServers, (s) => s.name, (s) => `${s.name}: ${describeMcp(s)}`, true));
  out.push(...diffKeyed("ACP provider", current.acpProviders, next.acpProviders, (e) => e.id, acp, true));
  out.push(...diffKeyed("Hook", current.hookEvents, next.hookEvents, (h) => h.event, (h) => `${h.event} (on error: ${h.onError ?? "open"})`, true));
  // Only profiles that need attention are part of the fingerprint; the calm ones are informational.
  const flagged = (c: PluginConsent) => (c.profiles ?? []).filter((p) => (p.escalations ?? []).length > 0);
  out.push(...diffKeyed("Escalating agent", flagged(current), flagged(next), (p) => p.name, (p) => `${p.name}: ${(p.escalations ?? []).join("; ")}`, true));
  const calm = (c: PluginConsent) => (c.profiles ?? []).filter((p) => (p.escalations ?? []).length === 0);
  out.push(...diffKeyed("Agent", calm(current), calm(next), (p) => p.name, (p) => p.name, false));
  out.push(...diffKeyed("Skill", current.skills?.map((name) => ({ name })), next.skills?.map((name) => ({ name })), (s) => s.name, (s) => s.name, false));
  return out;
}
