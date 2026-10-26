// slashRpc.ts: an RPC-backed slash command (ADR-0042 PB.4). Selecting one asks the plugin, through the daemon's
// plugin.call, for the prompt text and puts it in the draft in place of the `/token`. The phone executes nothing:
// the plugin runs on the daemon, the answer is untrusted text that is bounded, checked and inserted as plain
// characters, and nothing here (or in the suggestion list) can send.

import { DaemonError, RelayConnection } from '../relay/RelayConnection';
import { callPluginRpc } from './api';
import { findActiveSlash, SlashCommand } from './slashCommands';

/** Most text an RPC-backed command may insert; larger is refused, not cut. */
export const MAX_SLASH_RPC_PROMPT_CHARS = 16 * 1024;

/** `{prompt: string}`, non-empty, bounded, without NUL; anything else is null (a failure shown inline). */
export function slashRpcPrompt(result: unknown): string | null {
  if (typeof result !== 'object' || result === null) return null;
  const prompt = (result as { prompt?: unknown }).prompt;
  if (typeof prompt !== 'string' || prompt.trim() === '' || prompt.length > MAX_SLASH_RPC_PROMPT_CHARS || prompt.includes('\u0000')) return null;
  return prompt;
}

export type SlashRpcStatus = { key: string; state: 'running' } | { key: string; state: 'error'; message: string; code?: string };

export type SlashRpcOutcome = { ok: true; prompt: string } | { ok: false; message: string; code?: string };

/** Asks the plugin for the command's prompt. Never throws: a failure is a typed outcome for the list to show. */
export async function runSlashRpc(conn: RelayConnection | null, row: SlashCommand): Promise<SlashRpcOutcome> {
  const call = row.rpc;
  if (!call) return { ok: false, message: 'This command has no plugin call.' };
  if (!conn) return { ok: false, message: 'Not connected to the daemon.' };
  try {
    const result = await callPluginRpc(conn, call.plugin, call.rpc, { command: call.command, args: '' });
    const prompt = slashRpcPrompt(result);
    if (prompt === null) return { ok: false, message: `Plugin ${call.plugin} did not return prompt text for /${row.label}.` };
    return { ok: true, prompt };
  } catch (err) {
    const message = err instanceof Error ? err.message.replace(/^plugin\.call: /, '') : String(err);
    return { ok: false, message, code: err instanceof DaemonError ? err.code : undefined };
  }
}

/**
 * The draft with the plugin's text in place of the `/token`, or null when the draft no longer holds the token the
 * command was picked for (the user kept typing: the late answer is dropped). Never sends.
 */
export function applySlashRpcPrompt(draft: string, pickedToken: string, prompt: string): string | null {
  const slash = findActiveSlash({ text: draft, cursorIndex: draft.length });
  if (!slash || draft.slice(slash.start, slash.end) !== pickedToken) return null;
  return `${draft.slice(0, slash.start)}${prompt}${draft.slice(slash.end)}`;
}
