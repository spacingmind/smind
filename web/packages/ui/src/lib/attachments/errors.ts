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
    // ADR-0039 §6's forge vocabulary, for the PR/issue attachments the GitHub picker adds in a later PR.
    case "forge_cli_missing":
      return "GitHub is unavailable: gh is not installed on the daemon host.";
    case "forge_unauthenticated":
      return "GitHub is unavailable: gh is not logged in on the daemon host.";
    case "forge_no_remote":
      return "This workspace has no GitHub remote.";
    case "forge_not_found":
      return "That pull request or issue was not found.";
    case "forge_rate_limited":
      return "GitHub is rate limiting the daemon; try again in a few minutes.";
    case "forge_timeout":
      return "GitHub took too long to answer.";
    default:
      return message;
  }
}
