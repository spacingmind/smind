// slashCommands.ts: the composer's `/` list (ADR-0042 AC13). A PURE port of web's slash-commands.ts (not an import:
// the two apps share no code), kept test-for-test with it. Selecting a row INSERTS its prompt text; nothing here, and
// nothing in the suggestion list component, can send.

import { PluginCommand } from './types';

/** A command only the app itself defines. There are none on mobile yet; the precedence rule is in place for the first. */
export interface BuiltinSlashCommand {
  name: string;
  description: string;
  text: string;
}

export const BUILTIN_SLASH_COMMANDS: readonly BuiltinSlashCommand[] = [];

/** One row of the `/` list. */
export interface SlashCommand {
  key: string;
  /** What the row shows after the `/`: the bare name, or `<plugin>:<name>` when the bare name belongs to something else. */
  label: string;
  description: string;
  /** "built-in", or the id of the plugin the command is from (drawn as a quiet "from <plugin>"). */
  source: string;
  builtin: boolean;
  /** The text selecting the row inserts into the composer. */
  text: string;
  /** Names a query may match, lowercase. */
  names: string[];
}

/**
 * Built-ins first, then plugin commands in plugin-id order. Bare names are claimed in that order: a built-in's name is
 * never given to a plugin command, and between plugins the first by id keeps the bare `/name` while the rest are
 * reachable only as `/<plugin>:<name>`.
 */
export function buildSlashCommands(builtins: readonly BuiltinSlashCommand[], plugins: readonly PluginCommand[]): SlashCommand[] {
  const claimed = new Set<string>();
  const out: SlashCommand[] = [];
  for (const b of builtins) {
    const name = b.name.toLowerCase();
    if (claimed.has(name)) continue;
    claimed.add(name);
    out.push({ key: `builtin:${b.name}`, label: b.name, description: b.description, source: 'built-in', builtin: true, text: b.text, names: [name] });
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
      description: c.description ?? '',
      source: c.plugin,
      builtin: false,
      text: c.prompt,
      names: ownsBare ? [bare, qualified] : [qualified],
    });
  }
  return out;
}

/** The `/query` token at the start of the text: `text.slice(start, end)` is `/` plus the query. */
export interface SlashRange {
  start: number;
  end: number;
  query: string;
}

/**
 * Only a `/` that starts the composer text opens the list, so a path or a URL typed mid-prompt never does. The token
 * ends at the first whitespace; the caret must still be inside it. (The mobile field reports no selection, so callers
 * pass the end of the text: typing is at the end.)
 */
export function findActiveSlash({ text, cursorIndex }: { text: string; cursorIndex: number }): SlashRange | null {
  if (!text.startsWith('/')) return null;
  const cursor = Math.max(0, Math.min(cursorIndex, text.length));
  if (cursor < 1) return null;
  const before = text.slice(0, cursor);
  if (/\s/.test(before)) return null;
  return { start: 0, end: cursor, query: before.slice(1) };
}

/** Rows whose name starts with the query first, then those that contain it; an empty query lists everything. */
export function rankSlashCommands(commands: readonly SlashCommand[], query: string): SlashCommand[] {
  const q = query.toLowerCase();
  if (q === '') return [...commands];
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

/** The rows to show for the current draft: none unless the draft starts a `/token` and some command matches it. */
export function slashSuggestions(commands: readonly SlashCommand[], draft: string): SlashCommand[] {
  if (commands.length === 0) return [];
  const slash = findActiveSlash({ text: draft, cursorIndex: draft.length });
  return slash ? rankSlashCommands(commands, slash.query) : [];
}

/** The draft after picking `command`: the prompt text in place of the `/token`. A pure function of the draft; sending is not its business. */
export function pickSlashCommand(draft: string, command: SlashCommand): string {
  const slash = findActiveSlash({ text: draft, cursorIndex: draft.length });
  return slash ? applySlashReplacement({ text: draft, slash, command }).text : draft;
}
