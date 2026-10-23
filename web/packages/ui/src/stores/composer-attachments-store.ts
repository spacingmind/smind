import { create } from "zustand";

import { formatBytes } from "@/lib/attachments/format";
import { sameMentionRef, type MentionRef } from "@/lib/file-mention";
import type { AttachmentLimits } from "@/lib/types";

interface DraftBase {
  localId: string;
  /** Why this item cannot be sent (client cap); submit is blocked while any item has one. */
  error?: string;
}

/** A picked or pasted image/file: bytes stay in the `File` until submit. */
export interface DraftFile extends DraftBase {
  kind: "image" | "file";
  file: File;
  name: string;
  mimeType: string;
  size: number;
  /** Object URL of an image (revoked on remove/clear). */
  previewUrl?: string;
}

/** A picked pull request or issue: just the daemon-side reference plus what the pill shows. No bytes, no size cap. */
export interface DraftForgeRef extends DraftBase {
  kind: "pr" | "issue";
  forge: "github";
  number: number;
  repo: string;
  title: string;
  url: string;
}

export type DraftAttachment = DraftFile | DraftForgeRef;

export function isDraftFile(item: DraftAttachment): item is DraftFile {
  return item.kind === "image" || item.kind === "file";
}

/** What a pill and an aria-label call the item: the file name, or number plus title for a PR/issue. */
export function draftLabel(item: DraftAttachment): string {
  return isDraftFile(item) ? item.name : `#${item.number} ${item.title}`;
}

/** The store key: one draft tray per task and chat. */
export function attachmentDraftKey(taskId: number, chatId: number): string {
  return `${taskId}:${chatId}`;
}

const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const NONE: DraftAttachment[] = [];
const NO_MENTIONS: MentionRef[] = [];
const NO_TEXTS: string[] = [];

let nextId = 0;
let pastedImages = 0;

function revoke(item: DraftAttachment): void {
  if (isDraftFile(item) && item.previewUrl) URL.revokeObjectURL(item.previewUrl);
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
    // A PR/issue counts toward the item cap only: it carries no bytes.
    const size = isDraftFile(item) ? item.size : 0;
    const cap = item.kind === "image" ? limits.maxImageBytes : limits.maxFileBytes;
    if (isDraftFile(item) && size > cap) error = `Larger than ${formatBytes(cap)}`;
    else if (accepted >= limits.maxCount) error = `Over the ${limits.maxCount} attachment limit`;
    else if (total + size > limits.maxTotalBytes) error = `Over the ${formatBytes(limits.maxTotalBytes)} total`;
    if (!error) {
      accepted += 1;
      total += size;
    }
    return error === item.error ? item : { ...item, error };
  });
}

interface ComposerAttachmentsState {
  byKey: Record<string, DraftAttachment[]>;
  /** The refs picked from the `@` popover or added from the diff/Files panes, per key; only those whose exact token is still in the prompt are sent as `file_ref`s. */
  mentions: Record<string, MentionRef[]>;
  /** Refs waiting for the composer of that key to put their token into the draft. */
  pendingInserts: Record<string, MentionRef[]>;
  /** Prompt text waiting for the composer of that key to append it to its draft (never sent by itself). */
  pendingTexts: Record<string, string[]>;
  add: (key: string, files: File[], limits: AttachmentLimits) => void;
  /** Adds a picked PR/issue (a duplicate of one already in the tray is ignored). */
  addForgeRef: (key: string, ref: Omit<DraftForgeRef, "localId" | "error">, limits: AttachmentLimits) => void;
  addMention: (key: string, path: string) => void;
  /** Remembers a ref (deduped on path, line, endLine) without touching the draft. */
  rememberMention: (key: string, ref: MentionRef) => void;
  /** Asks the composer of `key` to append the ref's token to its draft. */
  queueInsert: (key: string, ref: MentionRef) => void;
  /** Returns and clears the queued inserts of `key`. */
  takeInserts: (key: string) => MentionRef[];
  /** Asks the composer of `key` to append `text` to its draft after a blank line. */
  queueText: (key: string, text: string) => void;
  /** Returns and clears the queued texts of `key`. */
  takeTexts: (key: string) => string[];
  clearMentions: (key: string) => void;
  remove: (key: string, localId: string, limits?: AttachmentLimits | null) => void;
  /** Drops the tray and revokes its object URLs (on unmount). */
  clear: (key: string) => void;
  /** Removes the sent items after a successful send WITHOUT revoking: the optimistic run entry now owns the preview URLs. */
  release: (key: string, localIds: string[]) => void;
}

/**
 * The composer's draft attachments (ADR-0038 §7), in memory and keyed by task and chat: the composer, the drop
 * overlay on the whole task pane and the toolbar all read it. Holds `File` objects and object URLs, never base64.
 */
export const useComposerAttachmentsStore = create<ComposerAttachmentsState>()((set, get) => ({
  byKey: {},
  mentions: {},
  pendingInserts: {},
  pendingTexts: {},
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
  addForgeRef: (key, ref, limits) => {
    const current = get().byKey[key] ?? NONE;
    if (current.some((i) => i.kind === ref.kind && !isDraftFile(i) && i.number === ref.number && i.repo === ref.repo)) return;
    const added: DraftForgeRef = { ...ref, localId: `att-${++nextId}` };
    set({ byKey: { ...get().byKey, [key]: validate([...current, added], limits) } });
  },
  addMention: (key, path) => get().rememberMention(key, { path }),
  rememberMention: (key, ref) => {
    const held = get().mentions[key] ?? NO_MENTIONS;
    if (held.some((h) => sameMentionRef(h, ref))) return;
    set({ mentions: { ...get().mentions, [key]: [...held, ref] } });
  },
  queueInsert: (key, ref) => {
    const held = get().pendingInserts[key] ?? NO_MENTIONS;
    if (held.some((h) => sameMentionRef(h, ref))) return;
    set({ pendingInserts: { ...get().pendingInserts, [key]: [...held, ref] } });
  },
  takeInserts: (key) => {
    const held = get().pendingInserts[key];
    if (!held) return NO_MENTIONS;
    const { [key]: _gone, ...rest } = get().pendingInserts;
    set({ pendingInserts: rest });
    return held;
  },
  queueText: (key, text) => set({ pendingTexts: { ...get().pendingTexts, [key]: [...(get().pendingTexts[key] ?? NO_TEXTS), text] } }),
  takeTexts: (key) => {
    const held = get().pendingTexts[key];
    if (!held) return NO_TEXTS;
    const { [key]: _gone, ...rest } = get().pendingTexts;
    set({ pendingTexts: rest });
    return held;
  },
  clearMentions: (key) => {
    if (!get().mentions[key]) return;
    const { [key]: _gone, ...rest } = get().mentions;
    set({ mentions: rest });
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
    // Mentions are not cleared here: the chat tab unmounts whenever another tab is in front, while the draft TEXT (and
    // so the `@path` tokens) survives. A mention is only sent while its token is still in the text, so a stale one is
    // harmless; a send clears them (clearMentions).
    const current = get().byKey[key];
    if (!current) return;
    current.forEach(revoke);
    const { [key]: _gone, ...rest } = get().byKey;
    set({ byKey: rest });
  },
  release: (key, localIds) => {
    const left = (get().byKey[key] ?? NONE).filter((i) => !localIds.includes(i.localId));
    if (left.length > 0) set({ byKey: { ...get().byKey, [key]: left } });
    else {
      const { [key]: _gone, ...rest } = get().byKey;
      set({ byKey: rest });
    }
  },
}));

/** The draft tray of one key (a stable empty array when there is none). */
export function useDraftAttachments(key: string | null): DraftAttachment[] {
  return useComposerAttachmentsStore((s) => (key ? (s.byKey[key] ?? NONE) : NONE));
}

/** The refs remembered for one key (a stable empty array when there are none). */
export function useDraftMentions(key: string | null): MentionRef[] {
  return useComposerAttachmentsStore((s) => (key ? (s.mentions[key] ?? NO_MENTIONS) : NO_MENTIONS));
}
