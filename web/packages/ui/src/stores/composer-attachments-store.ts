import { create } from "zustand";

import { formatBytes } from "@/lib/attachments/format";
import type { AttachmentLimits } from "@/lib/types";

export interface DraftAttachment {
  localId: string;
  file: File;
  kind: "image" | "file";
  name: string;
  mimeType: string;
  size: number;
  /** Object URL of an image (revoked on remove/clear). */
  previewUrl?: string;
  /** Why this item cannot be sent (client cap); submit is blocked while any item has one. */
  error?: string;
}

/** The store key: one draft tray per task and chat. */
export function attachmentDraftKey(taskId: number, chatId: number): string {
  return `${taskId}:${chatId}`;
}

const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const NONE: DraftAttachment[] = [];

let nextId = 0;
let pastedImages = 0;

function revoke(item: DraftAttachment): void {
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl);
}

/** A pasted image has no meaningful name: `clipboard-<n>.<ext>` (ADR-0038 §6). */
function nameOf(file: File): string {
  if (file.name && file.name !== "image.png") return file.name;
  const ext = (file.type.split("/")[1] ?? "png").replace("jpeg", "jpg");
  pastedImages += 1;
  return `clipboard-${pastedImages}.${ext}`;
}

/** Applies the client caps to the running list; the daemon's checks stay the authority. */
function validate(items: DraftAttachment[], limits: AttachmentLimits): DraftAttachment[] {
  // Only accepted items count toward the running count/total, so one oversize file does not fail the rest.
  let total = 0;
  let accepted = 0;
  return items.map((item) => {
    let error: string | undefined;
    const cap = item.kind === "image" ? limits.maxImageBytes : limits.maxFileBytes;
    if (item.size > cap) error = `Larger than ${formatBytes(cap)}`;
    else if (accepted >= limits.maxCount) error = `Over the ${limits.maxCount} attachment limit`;
    else if (total + item.size > limits.maxTotalBytes) error = `Over the ${formatBytes(limits.maxTotalBytes)} total`;
    if (!error) {
      accepted += 1;
      total += item.size;
    }
    return error === item.error ? item : { ...item, error };
  });
}

interface ComposerAttachmentsState {
  byKey: Record<string, DraftAttachment[]>;
  add: (key: string, files: File[], limits: AttachmentLimits) => void;
  remove: (key: string, localId: string, limits?: AttachmentLimits | null) => void;
  /** Drops the tray and revokes its object URLs (after a successful send, or on unmount). */
  clear: (key: string) => void;
}

/**
 * The composer's draft attachments (ADR-0038 §7), in memory and keyed by task and chat: the composer, the drop
 * overlay on the whole task pane and the toolbar all read it. Holds `File` objects and object URLs, never base64.
 */
export const useComposerAttachmentsStore = create<ComposerAttachmentsState>()((set, get) => ({
  byKey: {},
  add: (key, files, limits) => {
    if (files.length === 0) return;
    const added: DraftAttachment[] = files.map((file) => {
      const isImage = IMAGE_TYPES.includes(file.type) && limits.images.includes(file.type);
      return {
        localId: `att-${++nextId}`,
        file,
        kind: isImage ? "image" : "file",
        name: nameOf(file),
        mimeType: file.type || "application/octet-stream",
        size: file.size,
        previewUrl: isImage ? URL.createObjectURL(file) : undefined,
      };
    });
    set({ byKey: { ...get().byKey, [key]: validate([...(get().byKey[key] ?? NONE), ...added], limits) } });
  },
  remove: (key, localId, limits) => {
    const current = get().byKey[key] ?? NONE;
    const gone = current.find((i) => i.localId === localId);
    if (!gone) return;
    revoke(gone);
    let next = current.filter((i) => i.localId !== localId);
    // Removing an item can bring the running total back under its cap.
    if (limits) next = validate(next, limits);
    set({ byKey: { ...get().byKey, [key]: next } });
  },
  clear: (key) => {
    const current = get().byKey[key];
    if (!current) return;
    current.forEach(revoke);
    const { [key]: _gone, ...rest } = get().byKey;
    set({ byKey: rest });
  },
}));

/** The draft tray of one key (a stable empty array when there is none). */
export function useDraftAttachments(key: string | null): DraftAttachment[] {
  return useComposerAttachmentsStore((s) => (key ? (s.byKey[key] ?? NONE) : NONE));
}
