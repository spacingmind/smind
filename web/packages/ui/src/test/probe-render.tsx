import { act } from "react";
import { screen } from "@testing-library/react";

import { ProbeCard } from "@/components/learning/probe-card";
import type { ProbeEstimate, ProbeView } from "@/lib/probe-types";
import { estimate as estimateFixture } from "@/test/probe-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

export async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

export interface ProbeScript {
  probe?: ProbeView | null | Error;
  estimate?: ProbeEstimate | Error;
  start?: ProbeView | Error;
}

/** Renders an opened ProbeCard for proposal 9 against scripted `learning.probe.*` answers. */
export async function renderProbeCard(script: ProbeScript = {}) {
  const calls: { method: string; params: unknown }[] = [];
  const answer = <T,>(value: T | Error): T => {
    if (value instanceof Error) throw value;
    return value;
  };
  const d = learningDaemon((method, params) => {
    calls.push({ method, params });
    switch (method) {
      case "learning.probe.get":
        return answer(script.probe === undefined ? null : script.probe);
      case "learning.probe.estimate":
        return answer(script.estimate ?? estimateFixture());
      case "learning.probe.start":
        return answer(script.start ?? null);
      case "learning.probe.cancel":
        return {};
      default:
        return null;
    }
  });
  const view = renderWithDaemon(<ProbeCard id={9} workspaceId={1} />, d);
  await flush();
  await act(async () => screen.getByTestId("probe-toggle").click());
  await flush();
  return { ...view, calls, daemon: d };
}
