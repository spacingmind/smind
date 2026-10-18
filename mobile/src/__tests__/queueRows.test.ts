// queueRows.test.ts covers the queue row labels' pure logic (ADR-0021 §9),
// mirroring the web composer's queued-prompts.tsx wording.

import { describe, expect, it } from 'vitest';
import { cancelAccessibilityLabel, queueRowLabel } from '../queueRows';

describe('queueRowLabel', () => {
  it('a queued item reads QUEUED and a priority item reads NEXT', () => {
    expect(queueRowLabel({ Status: 'queued', Priority: 0 })).toBe('QUEUED');
    expect(queueRowLabel({ Status: 'queued', Priority: 1 })).toBe('NEXT');
  });

  it('an auto-cancelled item reads NOT SENT', () => {
    expect(queueRowLabel({ Status: 'cancelled', Priority: 0 })).toBe('NOT SENT');
  });

  it('the cancel label names the first 40 characters of the prompt', () => {
    expect(cancelAccessibilityLabel({ Prompt: 'short prompt' })).toBe('Cancel queued prompt: short prompt');
    const long = 'x'.repeat(60);
    expect(cancelAccessibilityLabel({ Prompt: long })).toBe(`Cancel queued prompt: ${'x'.repeat(40)}`);
  });
});
