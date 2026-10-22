import type { AttachmentLimits } from "@/lib/types";

/** What a daemon that does not list `kinds` accepts (wire contract: ABSENT = these three). */
const DEFAULT_KINDS = ["image", "file", "file_ref"];

/** The attachment kinds the daemon accepts, so the UI offers no kind it would reject. */
export function attachmentKinds(limits: AttachmentLimits | null): string[] {
  if (!limits) return [];
  return limits.kinds ?? DEFAULT_KINDS;
}

export function supportsKind(limits: AttachmentLimits | null, kind: string): boolean {
  return attachmentKinds(limits).includes(kind);
}
