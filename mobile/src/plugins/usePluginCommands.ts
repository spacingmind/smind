// usePluginCommands: the composer's `/` rows. Any failure (an older daemon without plugin.commands, a dropped
// connection) is simply "no commands": the composer is then unchanged.

import { useMemo } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { fetchPluginCommands } from './api';
import { BUILTIN_SLASH_COMMANDS, buildSlashCommands, SlashCommand } from './slashCommands';
import { usePluginResource } from './usePluginResource';

export function usePluginCommands(conn: RelayConnection | null): SlashCommand[] {
  const { state } = usePluginResource(conn, fetchPluginCommands);
  return useMemo(() => (state.kind === 'ready' ? buildSlashCommands(BUILTIN_SLASH_COMMANDS, state.value) : []), [state]);
}
