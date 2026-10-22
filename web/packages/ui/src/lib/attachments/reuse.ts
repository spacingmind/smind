import type { AttachmentMeta, AttachmentRequest } from "@/lib/types";

/**
 * The requests that re-send an earlier run's attachments (Re-run, Continue, Retry with higher effort; ADR-0038 §1):
 * stored images/files are carried over by `reuse` (the daemon copies them into a new set), and the daemon-side kinds
 * are rebuilt from their metadata. An item that cannot be re-sent (no number, no path) is skipped.
 */
export function resendRequests(runId: string, attachments: AttachmentMeta[] | undefined): AttachmentRequest[] {
  const out: AttachmentRequest[] = [];
  for (const a of attachments ?? []) {
    if (a.kind === "image" || a.kind === "file") out.push({ kind: "reuse", runId, id: a.id });
    else if (a.kind === "file_ref" && a.path) out.push({ kind: "file_ref", path: a.path });
    else if ((a.kind === "github_pr" || a.kind === "github_issue") && a.number) {
      out.push({ kind: a.kind, number: a.number, ...(a.repo ? { repo: a.repo } : {}) });
    }
  }
  return out;
}
