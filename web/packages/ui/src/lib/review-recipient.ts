import { readRunConfigPreference } from "@/components/composer/run-config-preference";
import { supportsThinking } from "@/lib/thinking-levels";
import type { Chat, Provider, ThinkingLevel } from "@/lib/types";

/** Where a submitted review goes: one chat, with the run config that chat is using. */
export interface ReviewRecipient {
  chatId: number;
  title: string;
  /** null for a chat that has never run and has no saved run config -- the caller falls back to the catalog's first provider, as the composer's default does. */
  provider: Provider | null;
  /** Empty = the provider's default (nothing is sent). */
  model: string;
  permissionMode: string;
  autoAccept: boolean;
  thinkingLevel: ThinkingLevel;
}

export type ReviewRecipientResult =
  | { kind: "ready"; recipient: ReviewRecipient }
  | { kind: "unavailable"; reason: "no-chats" | "disconnected" };

/**
 * Ported from Paseo's review/feedback recipient resolution (refs/paseo
 * packages/app/src/review/feedback/index.ts: `deriveState`'s
 * "no-agents"/"disconnected" reasons, the recipient list, one recipient
 * -> send directly). A review is addressed to a *chat* -- the daemon's
 * unit of conversation -- and carries that chat's own run config, never
 * "the first provider the daemon lists":
 *
 *  - recipient: the focused chat when it is still open (not archived),
 *    else the task's default (oldest) chat;
 *  - provider: the chat's bound provider (immutable after its first run),
 *    else the provider in its saved run config (the composer toolbar's
 *    per-chat state, run-config-preference.ts);
 *  - model / permission mode / auto-accept / thinking level: that saved
 *    run config, only when it belongs to the same provider -- the same
 *    provider-scoping the toolbar applies.
 *
 * Paseo's choosing/sending/sent phases don't carry over: smind always has
 * exactly one focused chat, so there is nothing to choose between.
 */
export function resolveReviewRecipient({
  taskId,
  chats,
  focusedChatId,
  connected,
}: {
  taskId: number;
  chats: readonly Chat[] | null | undefined;
  focusedChatId: number | null;
  connected: boolean;
}): ReviewRecipientResult {
  const open = (chats ?? []).filter((c) => c.ArchivedAt === null);
  if (open.length === 0) return { kind: "unavailable", reason: "no-chats" };
  if (!connected) return { kind: "unavailable", reason: "disconnected" };

  const chat = open.find((c) => c.ID === focusedChatId) ?? open[0]!;
  const isDefaultChat = chat.ID === open[0]!.ID;
  const config = readRunConfigPreference(taskId, chat.ID, isDefaultChat);
  const bound = (chat.Provider as Provider | null) || null;
  const provider = bound ?? config?.provider ?? null;
  const own = config !== null && config.provider === provider ? config : null;
  return {
    kind: "ready",
    recipient: {
      chatId: chat.ID,
      title: chat.Title,
      provider,
      model: own?.model ?? "",
      permissionMode: own?.permissionMode ?? "",
      autoAccept: own?.autoAccept ?? false,
      thinkingLevel: provider !== null && supportsThinking(provider) ? (own?.thinkingLevel ?? "") : "",
    },
  };
}

/** The run.start run-config params for a recipient -- omitted when empty, exactly as the composer's submit does (use-run-timeline.ts). */
export function recipientRunParams(recipient: ReviewRecipient): Record<string, unknown> {
  return {
    ...(recipient.permissionMode ? { permissionMode: recipient.permissionMode } : {}),
    ...(recipient.autoAccept ? { autoAccept: true } : {}),
    ...(recipient.thinkingLevel ? { thinkingLevel: recipient.thinkingLevel } : {}),
    ...(recipient.model ? { model: recipient.model } : {}),
  };
}
