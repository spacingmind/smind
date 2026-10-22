import { afterEach, describe, expect, it } from "vitest";

import { runConfigStorageKey } from "@/components/composer/run-config-preference";
import { recipientRunParams, resolveReviewRecipient } from "@/lib/review-recipient";
import type { Chat } from "@/lib/types";

function chat(id: number, over: Partial<Chat> = {}): Chat {
  return { ID: id, TaskID: 1, Title: `Chat ${id}`, Provider: null, AgentSession: null, CreatedAt: "", ArchivedAt: null, ...over };
}

function saveConfig(chatId: number, over: Record<string, unknown>): void {
  window.localStorage.setItem(
    runConfigStorageKey(1, chatId),
    JSON.stringify({ baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "", ...over }),
  );
}

afterEach(() => window.localStorage.clear());

describe("resolveReviewRecipient", () => {
  it("is unavailable without an open chat, and while disconnected", () => {
    expect(resolveReviewRecipient({ taskId: 1, chats: [], focusedChatId: null, connected: true })).toEqual({ kind: "unavailable", reason: "no-chats" });
    expect(resolveReviewRecipient({ taskId: 1, chats: null, focusedChatId: null, connected: true })).toEqual({ kind: "unavailable", reason: "no-chats" });
    expect(resolveReviewRecipient({ taskId: 1, chats: [chat(1)], focusedChatId: 1, connected: false })).toEqual({ kind: "unavailable", reason: "disconnected" });
  });

  it("picks the focused chat, else the default (oldest) one; archived chats are never recipients", () => {
    const chats = [chat(1), chat(2), chat(3, { ArchivedAt: "2024-01-01T00:00:00Z" })];
    const pick = (focusedChatId: number | null) => {
      const r = resolveReviewRecipient({ taskId: 1, chats, focusedChatId, connected: true });
      return r.kind === "ready" ? r.recipient.chatId : null;
    };
    expect(pick(2)).toBe(2);
    expect(pick(null)).toBe(1);
    expect(pick(3)).toBe(1);
  });

  it("uses the chat's bound provider over its saved config, and only same-provider config for model/mode", () => {
    saveConfig(2, { provider: "claude-native", model: "sonnet", permissionMode: "plan" });
    const r = resolveReviewRecipient({ taskId: 1, chats: [chat(1), chat(2, { Provider: "glm" })], focusedChatId: 2, connected: true });
    expect(r.kind === "ready" && r.recipient).toMatchObject({ chatId: 2, provider: "glm", model: "", permissionMode: "" });
  });

  it("carries the saved model, permission mode, auto-accept and (Claude only) thinking level", () => {
    saveConfig(2, { provider: "claude-native", model: "opus", permissionMode: "acceptEdits", autoAccept: true, thinkingLevel: "extended" });
    const r = resolveReviewRecipient({ taskId: 1, chats: [chat(1), chat(2)], focusedChatId: 2, connected: true });
    if (r.kind !== "ready") throw new Error("expected ready");
    expect(recipientRunParams(r.recipient)).toEqual({ permissionMode: "acceptEdits", autoAccept: true, thinkingLevel: "extended", model: "opus" });

    saveConfig(2, { provider: "glm", model: "glm-5", thinkingLevel: "extended" });
    const glm = resolveReviewRecipient({ taskId: 1, chats: [chat(1), chat(2)], focusedChatId: 2, connected: true });
    if (glm.kind !== "ready") throw new Error("expected ready");
    expect(recipientRunParams(glm.recipient)).toEqual({ model: "glm-5" });
  });

  it("leaves the provider null for a chat with neither a bound provider nor saved config", () => {
    const r = resolveReviewRecipient({ taskId: 1, chats: [chat(1)], focusedChatId: 1, connected: true });
    expect(r.kind === "ready" && r.recipient.provider).toBeNull();
  });
});
