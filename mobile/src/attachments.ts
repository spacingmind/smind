// attachments.ts is the UI-free half of ADR-0038's mobile prompt
// attachments: the relay request budget (the exact JSON byte length, not an
// estimate), the image downscale ladder, picker-result validation, daemon
// error wording and the timeline view helpers. Nothing here imports
// react-native or expo: the image manipulator and the file reader are
// injected, so the whole pipeline runs under vitest.

import type { AttachmentMeta, AttachmentPayload, AttachmentsInfo } from './api';

/** One E2EE relay frame minus its envelope: the largest run.start request the connection carries (internal/wsapi/attachments.go). */
export const RELAY_MAX_REQUEST_BYTES = 1048551;
/** What that leaves for attachment bytes once base64 (4/3) and the JSON around it are paid for; used in user-facing sentences. */
export const RELAY_PAYLOAD_HINT = '780 KB';
export const MAX_LONG_EDGE = 1568;
export const QUALITY_START = 0.8;
export const QUALITY_STEP = 0.1;
export const QUALITY_FLOOR = 0.4;
export const MAX_ATTACHMENTS = 10;

const PASSTHROUGH_IMAGES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];

export interface PendingAttachment {
  id: string;
  kind: 'image' | 'file';
  name: string;
  mimeType: string;
  /** base64 */
  data: string;
  /** decoded bytes */
  size: number;
  width?: number;
  height?: number;
  /** Thumbnail source, optional. */
  uri?: string;
}

/** What a picker returned, before any checking. */
export interface Pick {
  uri: string;
  name: string;
  mimeType?: string;
  size?: number;
  width?: number;
  height?: number;
  kind: 'image' | 'file';
}

export interface Refusal {
  ok: false;
  message: string;
}

export type AddResult = { ok: true; attachment: PendingAttachment } | Refusal;

/** The run.start fields besides attachments; the same ones api.startRunWhenBusy sends. */
export interface RunStartBase {
  taskId: number;
  provider: string;
  prompt: string;
  chatId?: number;
  whenBusy?: string;
  model?: string;
  permissionMode?: string;
}

export interface ImageDeps {
  manipulate(uri: string, opts: { maxLongEdge: number; quality: number }): Promise<{ uri: string; width: number; height: number; base64: string }>;
  readBase64(uri: string): Promise<string>;
}

export interface FileDeps {
  readBase64(uri: string): Promise<string>;
}

export interface AddContext {
  base: RunStartBase;
  existing: PendingAttachment[];
}

const encoder = new TextEncoder();

/** The exact UTF-8 byte length of the JSON request. */
export function requestBytes(params: object): number {
  return encoder.encode(JSON.stringify(params)).length;
}

/** The params the api layer will send, so the size check is on the real request (attachments omitted when empty, as api.ts does). */
export function runStartParams(base: RunStartBase, attachments: PendingAttachment[]): Record<string, unknown> {
  const params: Record<string, unknown> = { taskId: base.taskId, provider: base.provider, prompt: base.prompt };
  if (base.whenBusy !== undefined) params.whenBusy = base.whenBusy;
  if (base.chatId !== undefined) params.chatId = base.chatId;
  if (base.model) params.model = base.model;
  if (base.permissionMode) params.permissionMode = base.permissionMode;
  if (attachments.length > 0) {
    params.attachments = attachments.map(
      (a): AttachmentPayload => ({ kind: a.kind, name: a.name, mimeType: a.mimeType, data: a.data }),
    );
  }
  return params;
}

export function fits(base: RunStartBase, attachments: PendingAttachment[]): boolean {
  return requestBytes(runStartParams(base, attachments)) <= RELAY_MAX_REQUEST_BYTES;
}

/** Re-check at Send: null when the request fits, else the sentence to show. */
export function checkSend(base: RunStartBase, attachments: PendingAttachment[]): string | null {
  if (fits(base, attachments)) return null;
  const largest = attachments.reduce<PendingAttachment | null>((m, a) => (m === null || a.size > m.size ? a : m), null);
  if (largest === null) return `This prompt is too long to send from the phone; over the relay a prompt can carry about ${RELAY_PAYLOAD_HINT}.`;
  return `This prompt is too large to send: ${largest.name} (${humanSize(largest.size)}) is the largest attachment; over the relay a prompt can carry about ${RELAY_PAYLOAD_HINT}. Remove an attachment or shorten the prompt.`;
}

/** What the attach menu may offer. `info` undefined (not loaded, or an older daemon without the field) means image + file. */
export function capabilities(info?: AttachmentsInfo): { image: boolean; file: boolean; maxCount: number } {
  const kinds = info?.kinds;
  return {
    image: kinds === undefined ? true : kinds.includes('image'),
    file: kinds === undefined ? true : kinds.includes('file'),
    maxCount: info?.maxCount && info.maxCount > 0 ? info.maxCount : MAX_ATTACHMENTS,
  };
}

/** Decimal units: "512 B", "780 KB", "2.4 MB". */
export function humanSize(bytes: number): string {
  if (bytes < 1000) return `${bytes} B`;
  const kb = Math.round(bytes / 1000);
  if (kb < 1000) return `${kb} KB`;
  return `${(bytes / 1_000_000).toFixed(1)} MB`;
}

/** Decoded byte length of a base64 string. */
function base64Bytes(b64: string): number {
  let pad = 0;
  if (b64.endsWith('==')) pad = 2;
  else if (b64.endsWith('=')) pad = 1;
  return Math.floor((b64.length * 3) / 4) - pad;
}

let counter = 0;
function newId(): string {
  counter += 1;
  return `att-${Date.now().toString(36)}-${counter}`;
}

function jpgName(name: string): string {
  const dot = name.lastIndexOf('.');
  const stem = dot > 0 ? name.slice(0, dot) : name;
  return `${stem}.jpg`;
}

export async function prepareImage(pick: Pick, deps: ImageDeps, ctx: AddContext): Promise<AddResult> {
  const mimeType = (pick.mimeType ?? '').toLowerCase();
  const longEdge = pick.width !== undefined && pick.height !== undefined ? Math.max(pick.width, pick.height) : undefined;
  if (PASSTHROUGH_IMAGES.includes(mimeType) && longEdge !== undefined && longEdge <= MAX_LONG_EDGE) {
    const data = await deps.readBase64(pick.uri);
    const attachment: PendingAttachment = {
      id: newId(),
      kind: 'image',
      name: pick.name,
      mimeType,
      data,
      size: base64Bytes(data),
      width: pick.width,
      height: pick.height,
      uri: pick.uri,
    };
    if (fits(ctx.base, [...ctx.existing, attachment])) return { ok: true, attachment };
  }
  for (let step = 0; ; step++) {
    const quality = Math.round((QUALITY_START - step * QUALITY_STEP) * 100) / 100;
    if (quality < QUALITY_FLOOR - 1e-9) break;
    const out = await deps.manipulate(pick.uri, { maxLongEdge: MAX_LONG_EDGE, quality });
    const attachment: PendingAttachment = {
      id: newId(),
      kind: 'image',
      name: jpgName(pick.name),
      mimeType: 'image/jpeg',
      data: out.base64,
      size: base64Bytes(out.base64),
      width: out.width,
      height: out.height,
      uri: out.uri,
    };
    if (fits(ctx.base, [...ctx.existing, attachment])) return { ok: true, attachment };
  }
  return { ok: false, message: `${pick.name} is too large to send from the phone even after shrinking; try a smaller image` };
}

export async function prepareFile(pick: Pick, deps: FileDeps, ctx: AddContext): Promise<AddResult> {
  const data = await deps.readBase64(pick.uri);
  const size = base64Bytes(data);
  const attachment: PendingAttachment = {
    id: newId(),
    kind: 'file',
    name: pick.name,
    mimeType: pick.mimeType || 'application/octet-stream',
    data,
    size,
  };
  if (!fits(ctx.base, [...ctx.existing, attachment])) {
    return { ok: false, message: `${pick.name} is ${humanSize(size)}; over the relay a prompt can carry about ${RELAY_PAYLOAD_HINT}` };
  }
  return { ok: true, attachment };
}

export async function addPick(pick: Pick, deps: ImageDeps, ctx: AddContext & { max: number }): Promise<AddResult> {
  if (ctx.existing.length >= ctx.max) return { ok: false, message: `You can attach up to ${ctx.max} files to one prompt.` };
  return pick.kind === 'image' ? prepareImage(pick, deps, ctx) : prepareFile(pick, deps, ctx);
}

/**
 * The daemon's attachment error as an inline sentence. The error envelope
 * carries only message and code (no index field), but attach.Error's text
 * starts "attachment N: ", so the position is parsed from the message when
 * `index` is not given.
 */
export function attachmentErrorMessage(err: { code?: string; message?: string; index?: number }, names: string[]): string {
  let index = err.index;
  if (index === undefined) {
    const m = /^attachment (\d+):/.exec(err.message ?? '');
    if (m) index = Number(m[1]) - 1;
  }
  const name = index !== undefined && Number.isInteger(index) && index >= 0 && index < names.length ? names[index] : undefined;
  const subject = name ?? 'An attachment';
  switch (err.code) {
    case 'attachment_too_large':
      return `${subject} is too large for the daemon to accept.`;
    case 'too_many_attachments':
      return 'Too many attachments on one prompt.';
    case 'unsupported_image_type':
      return `${subject} is an image type the daemon does not accept.`;
    case 'mime_mismatch':
      return `${subject} is not the kind of image its type claims.`;
    case 'invalid_base64':
      return `${subject} could not be read; try attaching it again.`;
    case 'invalid_attachment':
      return `${subject} was rejected by the daemon.`;
    case 'outside_workspace':
      return `${subject} is outside this workspace.`;
    case 'not_found':
      return `${subject} was not found.`;
    default:
      return err.message || 'Could not send the attachments.';
  }
}

export interface TimelineAttachment {
  id: string;
  label: string;
  sizeLabel: string;
  kind: string;
  thumbnail: boolean;
}

/** run.attachment replies base64 plus name and mime in one E2EE frame; mirrors internal/wsapi/attachments.go handleRunAttachment's estimate. */
export function canThumbnail(meta: AttachmentMeta): boolean {
  if (meta.kind !== 'image' || typeof meta.size !== 'number') return false;
  const est = (((meta.size + 2) / 3) | 0) * 4 + ((meta.name ?? '').length + (meta.mimeType ?? '').length) * 6 + 256;
  return est <= RELAY_MAX_REQUEST_BYTES;
}

export function timelineAttachmentView(meta: AttachmentMeta[] | undefined): TimelineAttachment[] {
  return (meta ?? []).map((m) => {
    const hasBytes = m.kind === 'image' || m.kind === 'file';
    const label = hasBytes
      ? (m.name ?? 'attachment')
      : m.kind === 'file_ref'
        ? (m.path ?? m.name ?? 'file')
        : m.number !== undefined
          ? `#${m.number}`
          : (m.title ?? m.name ?? m.kind);
    return {
      id: m.id,
      label,
      sizeLabel: hasBytes && typeof m.size === 'number' ? humanSize(m.size) : '',
      kind: m.kind,
      thumbnail: canThumbnail(m),
    };
  });
}
