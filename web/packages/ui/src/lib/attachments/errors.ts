import { RpcError } from "@/lib/ws-client";
import type { AttachmentLimits } from "@/lib/types";
import { formatBytes } from "@/lib/attachments/format";

/**
 * The daemon's structured attachment errors (ADR-0038 §1) as short sentences for the composer's inline error.
 * The daemon's message names the offending attachment; unknown codes keep that message.
 */
export function attachmentErrorMessage(err: unknown, limits: AttachmentLimits | null): string {
  const message = err instanceof Error ? err.message : String(err);
  const code = err instanceof RpcError ? err.code : undefined;
  switch (code) {
    case "attachment_too_large":
      return limits ? `An attachment is too large (up to ${formatBytes(limits.maxFileBytes)} each, ${formatBytes(limits.maxTotalBytes)} in total).` : message;
    case "too_many_attachments":
      return limits ? `Too many attachments (up to ${limits.maxCount}).` : message;
    case "unsupported_image_type":
      return "That image type is not supported (PNG, JPEG, GIF and WebP only).";
    case "mime_mismatch":
      return "An image's contents do not match its file type.";
    case "invalid_base64":
      return "Could not read an attachment.";
    default:
      return message;
  }
}
