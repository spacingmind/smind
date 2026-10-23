import { describe, expect, it } from "vitest";

import { safeHttpUrl } from "@/lib/safe-url";

describe("safeHttpUrl", () => {
  it("allows http and https", () => {
    expect(safeHttpUrl("https://github.com/o/r/pull/7")).toBe("https://github.com/o/r/pull/7");
    expect(safeHttpUrl("http://example.com/a")).toBe("http://example.com/a");
  });

  it.each([
    "javascript:alert(1)",
    " javascript:alert(1)",
    "JaVaScRiPt:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "vbscript:msgbox(1)",
    "file:///etc/passwd",
    "ftp://example.com/x",
    "//example.com/x",
    "/relative/path",
    "not a url",
    "",
  ])("rejects %j", (value) => {
    expect(safeHttpUrl(value)).toBeNull();
  });

  it("rejects missing values", () => {
    expect(safeHttpUrl(undefined)).toBeNull();
    expect(safeHttpUrl(null)).toBeNull();
  });
});
