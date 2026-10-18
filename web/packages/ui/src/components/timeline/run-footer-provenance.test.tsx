import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

const BASE = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const HEAD = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function run(overrides: Partial<RunFooterRun> = {}): RunFooterRun {
  return {
    id: "run-1",
    provider: "claude-native",
    status: "done",
    startedAt: "2024-01-01T00:00:00Z",
    finishedAt: "2024-01-01T00:00:05Z",
    stopReason: "end_turn",
    ...overrides,
  };
}

afterEach(() => cleanup());

describe("RunFooter provenance segment", () => {
  it("mounts the provenance segment when shas are known", () => {
    render(<RunFooter run={run({ baseSha: BASE, headSha: HEAD })} />);
    expect(screen.getByTestId("run-provenance").textContent).toContain("bbbbbbb");
  });

  it("mounts nothing when neither sha is known", () => {
    render(<RunFooter run={run()} />);
    expect(screen.queryByTestId("run-provenance")).toBeNull();
  });
});
