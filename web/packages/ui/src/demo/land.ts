/** The route of the tour's live task (workspace 1, task 1, its chat): where the demo opens when the URL names nothing else. */
export const DEMO_LANDING_HASH = "#/workspace/1/task/1/chat/1";

/** ADR-0051: opens the live task on first paint, so a visitor sees the run streaming without clicking. */
export function landOnLiveTask(loc: Location = window.location, history: History = window.history): void {
  if (loc.hash === "" || loc.hash === "#") history.replaceState(null, "", `${loc.pathname}${loc.search}${DEMO_LANDING_HASH}`);
}
