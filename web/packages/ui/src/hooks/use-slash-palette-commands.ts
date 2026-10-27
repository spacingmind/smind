import { useMemo } from "react";

import { usePluginCommands } from "@/hooks/use-plugin-contributions";
import { buildSlashCommands, BUILTIN_SLASH_COMMANDS } from "@/lib/slash-commands";
import { useComposerInsertStore } from "@/stores/composer-insert-store";
import type { Command } from "@/palette/commands";
import { useCommands } from "@/palette/palette-provider";

/**
 * "Insert command: /x" palette entries for the slash commands (plugins' and built-ins), the same rows the
 * composer's `/` list shows. Running one puts the text in the composer that last had focus and never sends; with
 * no composer mounted there are no entries (nothing to insert into).
 */
export function useSlashPaletteCommands(): void {
  const pluginCommands = usePluginCommands();
  const hasComposer = useComposerInsertStore((state) => state.mounted.length > 0);
  const commands = useMemo<Command[]>(() => {
    if (!hasComposer) return [];
    // An rpc command has no text of its own to insert (the plugin produces it on request): the composer's `/` list runs it.
    return buildSlashCommands(BUILTIN_SLASH_COMMANDS, pluginCommands).filter((c) => !c.rpc).map((c) => ({
      id: `slash-${c.key}`,
      group: "Commands",
      title: `Insert command: /${c.label}`,
      subtitle: [c.description, c.builtin ? "" : `from ${c.source}`].filter(Boolean).join(" · ") || undefined,
      keywords: c.names,
      run: () => {
        useComposerInsertStore.getState().insert(c.text);
      },
    }));
  }, [hasComposer, pluginCommands]);
  useCommands("plugins:commands", 4, commands);
}
