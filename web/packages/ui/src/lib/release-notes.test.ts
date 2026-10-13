import { describe, expect, it } from "vitest";

import { tidyReleaseNotes } from "@/lib/release-notes";

const RAW = `## [0.11.0](https://github.com/spacingmind/smind/compare/v0.10.0...v0.11.0) (2026-10-13)


### Features

* bundled orchestration skills ([#279](https://github.com/spacingmind/smind/issues/279)) ([90469fb](https://github.com/spacingmind/smind/commit/90469fbc3b1eb78280108cee6dd8fb67dbc833dd))
* **desktop:** native Windows daemon ([#311](https://github.com/spacingmind/smind/issues/311)) ([aaf0cd9](https://github.com/spacingmind/smind/commit/aaf0cd940e756439eda766f28b206bb72f3aaf44))`;

describe("tidyReleaseNotes", () => {
  it("drops the version heading and commit-hash links, keeps PR links", () => {
    const out = tidyReleaseNotes(RAW);
    expect(out.startsWith("### Features")).toBe(true);
    expect(out).not.toContain("/commit/");
    expect(out).toContain("([#279](https://github.com/spacingmind/smind/issues/279))");
    expect(out).toContain("* **desktop:** native Windows daemon ([#311]");
  });

  it("leaves hand-written notes alone", () => {
    expect(tidyReleaseNotes("- Fixed a bug\n- Added another")).toBe("- Fixed a bug\n- Added another");
  });
});
