// adoptedPrompt.ts learns the text of a prompt the daemon delivered from
// the chat queue (run.status adoption): the streamed events do not carry
// it, so the screen first asks the queue's snapshot (the delivered item
// that started the run) and falls back to the run's own Prompt from
// run.list. Pure: no React, no RelayConnection import -- callers pass
// the queue surface and a run-listing thunk.

/**
 * The delivered prompt text for an adopted run: the delivered queue
 * entry's Prompt when the snapshot has one, otherwise the run's own
 * Prompt from run.list. A failing run.list or nothing knowing the run
 * yields undefined rather than throwing.
 */
export async function adoptedPromptText(
  queue: { promptForRun(runId: string): string | undefined },
  listRuns: () => Promise<Array<{ ID: string; Prompt: string }>>,
  runId: string,
): Promise<string | undefined> {
  const queued = queue.promptForRun(runId);
  if (queued !== undefined && queued !== '') return queued;
  let runs: Array<{ ID: string; Prompt: string }>;
  try {
    runs = await listRuns();
  } catch {
    return undefined;
  }
  const prompt = runs.find((r) => r.ID === runId)?.Prompt;
  return prompt !== undefined && prompt !== '' ? prompt : undefined;
}
