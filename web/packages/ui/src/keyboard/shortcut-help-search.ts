import type { HelpRow, HelpSection } from "@/keyboard/shortcuts";

/**
 * Search filtering for the Shortcuts settings section, scaled down from
 * `refs/paseo/packages/app/src/keyboard/shortcut-help-search.ts`. Paseo
 * matches against every *step*'s combinatorial modifier aliases
 * ("cmd"/"command" x "ctrl"/"control" x ...) built fresh per query; smind's
 * rows already carry a single formatted `keys` string and the raw
 * `effectiveCombo`, so a query is matched against those plus a small fixed
 * set of word aliases for the tokens the raw combo actually spells (`Mod`,
 * `Alt`, `Cmd`, `Ctrl`) -- covering "search 'cmd' and 'command' both find
 * ⌘K" without rebuilding the alias set per row per query.
 */

const TOKEN_ALIASES: Record<string, string> = {
  Mod: "mod cmd command ctrl control",
  Cmd: "cmd command",
  Ctrl: "ctrl control",
  Alt: "alt option opt",
};

/** Extra searchable words for a raw combo string's modifier tokens -- `keys`/`effectiveCombo` alone don't spell "command" or "control" out. */
function comboAliases(effectiveCombo: string): string {
  const tokens = effectiveCombo.split(/[ +]/);
  return tokens
    .map((token) => TOKEN_ALIASES[token])
    .filter((alias): alias is string => alias !== undefined)
    .join(" ");
}

function rowSearchText(row: HelpRow): string {
  return [row.label, row.note, row.keys, row.effectiveCombo, row.effectiveCombo ? comboAliases(row.effectiveCombo) : null]
    .filter((part): part is string => part !== undefined && part !== null)
    .join(" ")
    .toLocaleLowerCase();
}

/**
 * `sections` narrowed to `query`, matching a row's label, note, and every
 * spelling of the keys that actually fire it. A section whose own title
 * matches keeps all of its rows, same as the old dialog's full listing.
 */
export function filterShortcutHelpSections(
  sections: readonly HelpSection[],
  query: string,
): HelpSection[] {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return [...sections];

  return sections.flatMap((section) => {
    if (section.title.toLocaleLowerCase().includes(normalized)) return [section];
    const rows = section.rows.filter((row) => rowSearchText(row).includes(normalized));
    return rows.length > 0 ? [{ ...section, rows }] : [];
  });
}
