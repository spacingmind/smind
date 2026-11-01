import { WsClient } from "@/lib/ws-client";

import { prefersReducedMotion, realClock, type DemoClock } from "./clock";
import { DemoSocket } from "./demo-socket";
import { buildTour } from "./scenario";
import { DemoSession } from "./session";
import type { DemoScenario } from "./types";

let session: DemoSession | null = null;

/** Test hook: forget the page-load session so the next connectDemo builds a fresh one. */
export function resetDemoSession(): void {
  session = null;
}

/**
 * ADR-0051: the `connect` prop `main.tsx` passes to `<App>` in the demo build. The session (the scripted daemon's state)
 * lives for the page load and survives a new client, so a reconnect does not rewind the tour.
 */
export async function connectDemo(options: { scenario?: DemoScenario; clock?: DemoClock; reducedMotion?: boolean } = {}): Promise<WsClient> {
  const clock = options.clock ?? realClock();
  session ??= new DemoSession(options.scenario ?? buildTour(clock.now()), clock, options.reducedMotion ?? prefersReducedMotion());
  return new WsClient(new DemoSocket(session));
}
