import { describe, expect, it } from "vitest";

import { PERMISSION_REASON_LABEL } from "@/components/timeline/permission-reason";

describe("PERMISSION_REASON_LABEL", () => {
  it.each([
    ["human", "You approved", "success"],
    ["auto_safe", "Auto-approved", "running"],
    ["timeout", "Timed out", "warning"],
    ["provider_cancellation", "Cancelled by provider", "warning"],
  ] as const)("resolves %s to label %j with status %j", (reason, label, status) => {
    expect(PERMISSION_REASON_LABEL[reason]).toEqual({ label, status });
  });

  it("resolves a reason this build has never heard of to undefined, not a crash", () => {
    expect(PERMISSION_REASON_LABEL["some_future_reason"]).toBeUndefined();
  });
});
