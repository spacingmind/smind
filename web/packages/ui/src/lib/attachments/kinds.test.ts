import { describe, expect, it } from "vitest";

import { attachmentKinds, supportsKind } from "@/lib/attachments/kinds";
import type { AttachmentLimits } from "@/lib/types";

const base: AttachmentLimits = { maxCount: 10, maxImageBytes: 1, maxFileBytes: 1, maxTotalBytes: 1, maxMessageBytes: 1, images: [] };

describe("attachmentKinds", () => {
  it("assumes image, file and file_ref when the daemon lists none", () => {
    expect(attachmentKinds(base)).toEqual(["image", "file", "file_ref"]);
    expect(supportsKind(base, "pr")).toBe(false);
    expect(supportsKind(base, "file_ref")).toBe(true);
  });
  it("uses the daemon's list when present", () => {
    const limits = { ...base, kinds: ["image", "file", "file_ref", "pr", "issue"] };
    expect(supportsKind(limits, "pr")).toBe(true);
    expect(supportsKind(limits, "issue")).toBe(true);
  });
  it("is empty without limits", () => {
    expect(attachmentKinds(null)).toEqual([]);
  });
});
