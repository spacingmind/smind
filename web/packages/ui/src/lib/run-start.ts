import type { AttachmentRequest, PermissionSettings, Provider, ThinkingLevel, WhenBusy } from "@/lib/types";

/**
 * The run.start params, shared by the composer's send (useRunTimeline's
 * submitPrompt) and the footer's "Retry on <profile>": optional fields are
 * left out when unset (permissionMode "" / autoAccept false / no thinking /
 * default model), so the daemon's own defaults apply.
 */
export function runStartParams(o: {
  taskId: number;
  chatId: number;
  provider: Provider;
  prompt: string;
  whenBusy: WhenBusy;
  permission?: PermissionSettings;
  thinkingLevel?: ThinkingLevel;
  model?: string;
  profileId?: number;
  attachments?: AttachmentRequest[];
  /** RFC 3339: hold the prompt in the chat's queue until then (human callers only). */
  notBefore?: string;
}): Record<string, unknown> {
  return {
    taskId: o.taskId,
    chatId: o.chatId,
    provider: o.provider,
    prompt: o.prompt,
    whenBusy: o.whenBusy,
    ...(o.permission?.permissionMode ? { permissionMode: o.permission.permissionMode } : {}),
    ...(o.permission?.autoAccept ? { autoAccept: true } : {}),
    ...(o.thinkingLevel ? { thinkingLevel: o.thinkingLevel } : {}),
    ...(o.model ? { model: o.model } : {}),
    ...(o.profileId !== undefined ? { profileId: o.profileId } : {}),
    ...(o.attachments && o.attachments.length > 0 ? { attachments: o.attachments } : {}),
    ...(o.notBefore ? { notBefore: o.notBefore } : {}),
  };
}
