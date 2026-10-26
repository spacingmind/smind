import type { PluginCommand } from "@/lib/plugin-contrib";

/** A command only the app itself defines. Selecting it inserts `text`, exactly like a plugin's. */
export interface BuiltinSlashCommand {
  name: string;
  description: string;
  text: string;
}

/**
 * The composer's built-in slash commands. There are none yet (ACP's `available_commands_update` is not surfaced);
 * the registry and its precedence rule are in place so the first one needs no other change: a built-in always wins
 * a name against a plugin command.
 */
export const BUILTIN_SLASH_COMMANDS: readonly BuiltinSlashCommand[] = [];

/** One row of the `/` list. */
export interface SlashCommand {
  /** Unique React/option key. */
  key: string;
  /** What the row shows after the `/`: the bare name, or `<plugin>:<name>` when the bare name belongs to something else. */
  label: string;
  description: string;
  /** "built-in", or the id of the plugin the command is from (shown as a quiet "from <plugin>"). */
  source: string;
  builtin: boolean;
  /** The text selecting the row inserts into the composer (empty for an rpc command: its text comes from the plugin). */
  text: string;
  /** Set for an RPC-backed plugin command: selecting it calls `plugin.call` and inserts the result's prompt. */
  rpc?: { plugin: string; rpc: string; command: string };
  /** Names a query may match, lowercase. */
  names: string[];
}

/**
 * Built-ins first, then plugin commands in plugin-id order. Bare names are claimed in that order: a built-in's name
 * is never given to a plugin command, and between plugins the first by id keeps the bare `/name` while the rest are
 * reachable only as `/<plugin>:<name>`. (The daemon computes the same alias; the client derives it again so a
 * built-in, which the daemon cannot know, can take precedence.)
 */
export function buildSlashCommands(builtins: readonly BuiltinSlashCommand[], plugins: readonly PluginCommand[]): SlashCommand[] {
  const claimed = new Set<string>();
  const out: SlashCommand[] = [];
  for (const b of builtins) {
    const name = b.name.toLowerCase();
    if (claimed.has(name)) continue;
    claimed.add(name);
    out.push({ key: `builtin:${b.name}`, label: b.name, description: b.description, source: "built-in", builtin: true, text: b.text, names: [name] });
  }
  const sorted = plugins
    .map((command, index) => ({ command, index }))
    .sort((a, b) => (a.command.plugin < b.command.plugin ? -1 : a.command.plugin > b.command.plugin ? 1 : a.index - b.index));
  for (const { command: c } of sorted) {
    const bare = c.name.toLowerCase();
    const qualified = c.qualified.toLowerCase();
    const ownsBare = !claimed.has(bare);
    if (ownsBare) claimed.add(bare);
    out.push({
      key: `plugin:${c.qualified}`,
      label: ownsBare ? c.name : c.qualified,
      description: c.description ?? "",
      source: c.plugin,
      builtin: false,
      text: c.prompt ?? "",
      ...(c.rpc ? { rpc: { plugin: c.plugin, rpc: c.rpc, command: c.qualified } } : {}),
      names: ownsBare ? [bare, qualified] : [qualified],
    });
  }
  return out;
}

/** The `/query` token at the start of the text with the caret inside it: `text.slice(start, end)` is `/` plus the query. */
export interface SlashRange {
  start: number;
  end: number;
  query: string;
}

/**
 * Only a `/` that starts the composer text opens the list, so a path or a URL typed mid-prompt never does. The
 * token ends at the first whitespace; the caret must still be inside it.
 */
export function findActiveSlash({ text, cursorIndex }: { text: string; cursorIndex: number }): SlashRange | null {
  if (!text.startsWith("/")) return null;
  const cursor = Math.max(0, Math.min(cursorIndex, text.length));
  if (cursor < 1) return null;
  const before = text.slice(0, cursor);
  if (/\s/.test(before)) return null;
  return { start: 0, end: cursor, query: before.slice(1) };
}

/** Rows whose name starts with the query first, then those that contain it; an empty query lists everything. */
export function rankSlashCommands(commands: readonly SlashCommand[], query: string): SlashCommand[] {
  const q = query.toLowerCase();
  if (q === "") return [...commands];
  const prefix: SlashCommand[] = [];
  const inside: SlashCommand[] = [];
  for (const c of commands) {
    if (c.names.some((n) => n.startsWith(q))) prefix.push(c);
    else if (c.names.some((n) => n.includes(q))) inside.push(c);
  }
  return [...prefix, ...inside];
}

/** Replaces the `/token` with the command's text; `cursor` is the caret position right after it. Never sends. */
export function applySlashReplacement({ text, slash, command }: { text: string; slash: SlashRange; command: SlashCommand }): { text: string; cursor: number } {
  const before = text.slice(0, slash.start);
  const after = text.slice(slash.end);
  return { text: `${before}${command.text}${after}`, cursor: before.length + command.text.length };
}

/** Most text an RPC-backed command may insert; the plugin's answer is untrusted, so anything larger is refused, not cut. */
export const MAX_SLASH_RPC_PROMPT_CHARS = 16 * 1024;

/**
 * What an RPC-backed command's `plugin.call` result must be: `{prompt: string}`, non-empty, bounded, without NUL.
 * Anything else is a failure the composer shows inline; the string is inserted as plain text and never interpreted
 * (the textarea shows characters, so markup in it stays markup).
 */
export function slashRpcPrompt(result: unknown): string | null {
  if (typeof result !== "object" || result === null) return null;
  const prompt = (result as { prompt?: unknown }).prompt;
  if (typeof prompt !== "string" || prompt.trim() === "" || prompt.length > MAX_SLASH_RPC_PROMPT_CHARS || prompt.includes("\u0000")) return null;
  return prompt;
}
