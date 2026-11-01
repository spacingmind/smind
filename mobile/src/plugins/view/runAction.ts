// runAction.ts executes the five view actions (ADR-0025/0042) a plugin
// panel's document asks for, mirroring web's usePluginActionRunner as a
// pure-TS decision function: every effect (the rpc connection, the
// composer inbox, panel navigation, the url confirm) arrives as a dep, so
// there is no react and no react-native here. The document is untrusted:
// parseAction runs FIRST and nothing it did not return is ever read; text
// only ever reaches the composer as an insert; a url only opens after the
// caller confirms the host recomputed from the url itself. Never throws:
// a failure is a typed outcome.

import type { PluginCommand } from '../types';
import { openUrlHost, parseAction, rpcRequest } from './actions';

/** Every effect the runner needs; the app supplies them, the tests record them. */
export interface ActionDeps {
  pluginId: string;
  panelIds: readonly string[];
  commands: readonly PluginCommand[];
  callRpc(req: { pluginId: string; rpc: string; params: Record<string, unknown> }): Promise<unknown>;
  insertPrompt(text: string): boolean;
  openPanel(panelId: string, context?: Record<string, unknown>): void;
  confirmOpenUrl(host: string, url: string): Promise<boolean>;
  openUrl(url: string): Promise<void>;
  refresh(): void;
}

export type ActionOutcome =
  | { kind: 'done' }
  | { kind: 'cancelled' }
  | { kind: 'refused'; reason: string }
  | { kind: 'error'; message: string; code?: string };

/** A thrown error as an outcome; DaemonError's code is read duck-typed (a `.code` string), so no relay import is needed. */
function errorOutcome(err: unknown): { kind: 'error'; message: string; code?: string } {
  const message = err instanceof Error ? err.message.replace(/^plugin\.call: /, '') : String(err);
  const rawCode = err instanceof Error ? (err as unknown as { code?: unknown }).code : undefined;
  return { kind: 'error', message, code: typeof rawCode === 'string' ? rawCode : undefined };
}

export async function runViewAction(
  raw: unknown,
  deps: ActionDeps,
  extra?: { formValues?: Record<string, string | boolean> },
): Promise<ActionOutcome> {
  const action = parseAction(raw);
  if (action === null) return { kind: 'refused', reason: 'Unrecognised action' };
  switch (action.kind) {
    case 'rpc': {
      try {
        await deps.callRpc(rpcRequest(deps.pluginId, action, extra?.formValues)); // pluginId is always the caller's own
        deps.refresh(); // the panel's document is the rpc's visible effect: refetch it
        return { kind: 'done' };
      } catch (err) {
        return errorOutcome(err);
      }
    }
    case 'prompt':
      return deps.insertPrompt(action.text) ? { kind: 'done' } : { kind: 'refused', reason: 'Prompt text was empty or too large' };
    case 'command': {
      const command = deps.commands.find(
        (c): c is PluginCommand & { prompt: string } => c.qualified === action.name && c.plugin === deps.pluginId && typeof c.prompt === 'string',
      );
      if (!command) return { kind: 'refused', reason: 'Unknown command' };
      return deps.insertPrompt(command.prompt) ? { kind: 'done' } : { kind: 'refused', reason: 'Prompt text was empty or too large' };
    }
    case 'openPanel': {
      if (!deps.panelIds.includes(action.panel)) return { kind: 'refused', reason: 'Unknown panel' };
      deps.openPanel(action.panel, action.context);
      return { kind: 'done' };
    }
    case 'openUrl': {
      const host = openUrlHost(action.url); // recomputed locally: the document's host field is never trusted
      if (host === null) return { kind: 'refused', reason: 'Not an http(s) url' };
      try {
        if (!(await deps.confirmOpenUrl(host, action.url))) return { kind: 'cancelled' }; // declined: the url is not opened
        await deps.openUrl(action.url);
      } catch (err) {
        return errorOutcome(err);
      }
      return { kind: 'done' };
    }
  }
}
