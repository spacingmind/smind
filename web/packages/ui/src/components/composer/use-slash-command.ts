import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { SLASH_LISTBOX_ID, slashOptionId } from "@/components/composer/slash-command-popover";
import { usePluginCommands } from "@/hooks/use-plugin-contributions";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { RpcError } from "@/lib/ws-client";
import {
  applySlashReplacement,
  buildSlashCommands,
  findActiveSlash,
  rankSlashCommands,
  slashRpcPrompt,
  BUILTIN_SLASH_COMMANDS,
  type BuiltinSlashCommand,
  type SlashCommand,
  type SlashRange,
} from "@/lib/slash-commands";

/** The state of the last RPC-backed command: running, or why it failed (shown inline under the list). */
export type SlashRpcStatus = { key: string; state: "running" } | { key: string; state: "error"; message: string; code?: string };

/**
 * The composer's `/` command state, shaped like `useFileMention`: the active token at the caret, the ranked rows,
 * the highlighted row and the key rules. Picking a row only reports the new text and caret to `onAccept`; nothing
 * here can send. With no commands (no plugin commands and no built-ins) or no match the list is not open, so the
 * composer behaves exactly as it did before commands existed.
 */
export function useSlashCommand({
  text,
  caret,
  enabled,
  onAccept,
  builtins = BUILTIN_SLASH_COMMANDS,
}: {
  text: string;
  caret: number;
  enabled: boolean;
  onAccept: (command: SlashCommand, next: { text: string; cursor: number }) => void;
  builtins?: readonly BuiltinSlashCommand[];
}) {
  const pluginCommands = usePluginCommands();
  const { client } = useDaemonConnection();
  const textRef = useRef(text);
  textRef.current = text;
  const [rpcStatus, setRpcStatus] = useState<SlashRpcStatus | null>(null);
  const generation = useRef(0);
  const all = useMemo(() => buildSlashCommands(builtins, pluginCommands), [builtins, pluginCommands]);
  const slash = useMemo(() => (enabled && all.length > 0 ? findActiveSlash({ text, cursorIndex: caret }) : null), [enabled, all, text, caret]);
  const rows = useMemo(() => (slash ? rankSlashCommands(all, slash.query) : []), [all, slash]);
  // Esc closes the list for this token only; typing a new `/` (or leaving it) re-arms it.
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    if (!slash && dismissed) setDismissed(false);
  }, [slash, dismissed]);
  const open = slash !== null && rows.length > 0 && !dismissed;

  const [active, setActive] = useState(0);
  const query = slash?.query;
  useEffect(() => setActive(0), [query]);
  const activeIndex = Math.min(active, Math.max(rows.length - 1, 0));

  // An error is about the token it was raised for: typing, moving off it or Esc clears it.
  useEffect(() => {
    setRpcStatus((s) => (s && s.state === "error" ? null : s));
  }, [query, slash === null]);

  /**
   * An RPC-backed command asks its plugin (`plugin.call`) for the text and puts it in the draft in place of the
   * `/token`: it never sends. The answer is untrusted: it must be `{prompt}` within the cap, and it is dropped
   * (not applied) when the draft no longer holds the token it was asked for.
   */
  async function runRpc(command: SlashCommand, range: SlashRange) {
    const call = command.rpc;
    if (!call) return;
    if (rpcStatus?.state === "running") return;
    if (!client) {
      setRpcStatus({ key: command.key, state: "error", message: "Not connected to the daemon." });
      return;
    }
    const token = textRef.current.slice(range.start, range.end);
    const mine = ++generation.current;
    setRpcStatus({ key: command.key, state: "running" });
    try {
      const result = await client.call<unknown>("plugin.call", { pluginId: call.plugin, rpc: call.rpc, params: { command: call.command, args: "" } });
      if (mine !== generation.current) return;
      const prompt = slashRpcPrompt(result);
      if (prompt === null) {
        setRpcStatus({ key: command.key, state: "error", message: `Plugin ${call.plugin} did not return prompt text for /${command.label}.` });
        return;
      }
      setRpcStatus(null);
      if (textRef.current.slice(range.start, range.end) !== token) return;
      onAccept(command, applySlashReplacement({ text: textRef.current, slash: range, command: { ...command, text: prompt } }));
    } catch (err) {
      if (mine !== generation.current) return;
      const message = err instanceof Error ? err.message.replace(/^plugin\.call: /, "") : String(err);
      setRpcStatus({ key: command.key, state: "error", message, code: err instanceof RpcError ? err.code : undefined });
    }
  }

  function pick(command: SlashCommand) {
    if (!slash) return;
    if (command.rpc) {
      void runRpc(command, slash);
      return;
    }
    onAccept(command, applySlashReplacement({ text, slash, command }));
  }

  /** Returns true when the list consumed the key; everything else falls through to the composer. */
  function interceptKey(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
    if (!open) return false;
    if (e.key === "Escape") {
      e.preventDefault();
      setDismissed(true);
      generation.current++; // Esc also abandons a command still waiting on its plugin
      setRpcStatus(null);
      return true;
    }
    if (e.metaKey || e.ctrlKey || e.altKey) return false;
    const composing = (e.nativeEvent as unknown as { isComposing?: boolean }).isComposing;
    if (composing) return false;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActive((activeIndex + step + rows.length) % rows.length);
      return true;
    }
    if ((e.key === "Enter" && !e.shiftKey) || (e.key === "Tab" && !e.shiftKey)) {
      e.preventDefault();
      pick(rows[activeIndex]!);
      return true;
    }
    return false;
  }

  return {
    open,
    rows,
    activeIndex,
    setActive,
    pick,
    interceptKey,
    rpcStatus,
    textareaProps: {
      role: "combobox",
      "aria-autocomplete": "list" as const,
      "aria-expanded": open,
      "aria-controls": open ? SLASH_LISTBOX_ID : undefined,
      "aria-activedescendant": open ? slashOptionId(activeIndex) : undefined,
    },
  };
}
