// adoptedPrompt.test.ts covers adoptedPromptText (adoptedPrompt.ts): the
// two sources of an adopted run's prompt -- the queue snapshot's delivered
// entry first, then the run's own Prompt from run.list -- and the failure
// mode where neither knows it or run.list itself rejects.

import { describe, expect, it } from 'vitest';
import { adoptedPromptText } from '../adoptedPrompt';

describe('adoptedPromptText', () => {
  it('uses the delivered queue entry\'s prompt when the snapshot has it', async () => {
    let listCalls = 0;
    const text = await adoptedPromptText(
      { promptForRun: (runId) => (runId === 'run-7' ? 'next step' : undefined) },
      async () => {
        listCalls++;
        return [];
      },
      'run-7',
    );
    expect(text).toBe('next step');
    expect(listCalls).toBe(0);
  });

  it('falls back to the run\'s own prompt from run.list', async () => {
    const text = await adoptedPromptText(
      { promptForRun: () => undefined },
      async () => [{ ID: 'run-7', Prompt: 'from run.list' }, { ID: 'run-8', Prompt: 'other' }],
      'run-7',
    );
    expect(text).toBe('from run.list');
  });

  it('is undefined when neither knows the run', async () => {
    const text = await adoptedPromptText(
      { promptForRun: () => undefined },
      async () => [{ ID: 'run-8', Prompt: 'other' }],
      'run-7',
    );
    expect(text).toBeUndefined();
  });

  it('a failing run.list gives undefined instead of throwing', async () => {
    const text = await adoptedPromptText(
      { promptForRun: () => undefined },
      async () => {
        throw new Error('relay: connection is closed');
      },
      'run-7',
    );
    expect(text).toBeUndefined();
  });
});
