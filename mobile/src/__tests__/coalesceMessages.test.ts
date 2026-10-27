import { describe, expect, it } from 'vitest';
import type { PermissionRequestState } from '../permissionRequests';
import type { TimelineLine } from '../runTimeline';
import { buildTranscript } from '../transcript/coalesceMessages';

const line = (key: string, role: string, text: string): TimelineLine => ({ key, role, text });
const perm = (requestId: string): PermissionRequestState => ({
  runId: 'r1',
  requestId,
  summary: 'Run ls?',
  options: [{ id: 'a', label: 'Allow', kind: 'allow_once' }],
  status: 'pending',
  error: null,
});

describe('buildTranscript', () => {
  it('an empty timeline is an empty transcript', () => {
    expect(buildTranscript([])).toEqual([]);
  });

  it('merges adjacent assistant chunks into one item keyed by the first line', () => {
    const items = buildTranscript([line('a', 'user', 'hi'), line('b', 'assistant', 'Hello, '), line('c', 'assistant', 'world!')]);
    expect(items).toEqual([
      { type: 'user', key: 'a', text: 'hi' },
      { type: 'assistant', key: 'b', text: 'Hello, world!' },
    ]);
  });

  it('an assistant line after a tool call starts a new item', () => {
    const tool: TimelineLine = { key: 't', role: '', text: 'tool: x', toolCall: { toolName: 'x', title: 'y', status: 'running' } };
    const items = buildTranscript([line('a', 'assistant', 'one'), tool, line('b', 'assistant', 'two')]);
    expect(items.map((i) => i.type)).toEqual(['assistant', 'tool', 'assistant']);
    expect(items[1]).toEqual({ type: 'tool', key: 't', toolName: 'x', title: 'y', status: 'running' });
    expect(items[2]).toMatchObject({ key: 'b', text: 'two' });
  });

  it('merges thinking separately from assistant text', () => {
    const items = buildTranscript([
      line('a', 'assistant (thinking)', 'hm '),
      line('b', 'assistant (thinking)', 'ok'),
      line('c', 'assistant', 'answer'),
    ]);
    expect(items).toEqual([
      { type: 'thinking', key: 'a', text: 'hm ok' },
      { type: 'assistant', key: 'c', text: 'answer' },
    ]);
  });

  it('keeps the key stable when a third chunk appends', () => {
    const base = [line('a', 'assistant', 'x'), line('b', 'assistant', 'y')];
    const before = buildTranscript(base);
    const after = buildTranscript([...base, line('c', 'assistant', 'z')]);
    expect(after[0].key).toBe(before[0].key);
    expect(after[0]).toMatchObject({ text: 'xyz' });
  });

  it('passes meta lines through in order', () => {
    const items = buildTranscript([line('a', 'assistant', 'hi'), line('b', '', 'done (end_turn)')]);
    expect(items.map((i) => i.type)).toEqual(['assistant', 'meta']);
  });

  it('appends permissions then the footer last', () => {
    const items = buildTranscript([line('a', 'user', 'go')], [perm('p1'), perm('p2')], { sentence: 'Stopped' });
    expect(items.map((i) => i.key)).toEqual(['a', 'perm-p1', 'perm-p2', 'footer']);
    expect(items[3]).toEqual({ type: 'footer', key: 'footer', sentence: 'Stopped' });
  });

  it('does not mutate its input', () => {
    const input = [line('a', 'assistant', 'x'), line('b', 'assistant', 'y')];
    const copy = JSON.parse(JSON.stringify(input));
    buildTranscript(input);
    expect(input).toEqual(copy);
  });
});

describe('buildTranscript footer note', () => {
  it('carries the note and waiting flag, and omits them when absent', () => {
    expect(buildTranscript([], [], { sentence: 'S', note: 'Queued · starts after 17:35', waiting: true })[0]).toEqual({
      type: 'footer',
      key: 'footer',
      sentence: 'S',
      note: 'Queued · starts after 17:35',
      waiting: true,
    });
    expect(buildTranscript([], [], { sentence: 'S' })[0]).toEqual({ type: 'footer', key: 'footer', sentence: 'S' });
  });
});

describe('buildTranscript error card', () => {
  const quota = '429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-27 22:44:59';
  const err = `${quota} (taskrunner: glm prompt: acp: rpc error -32603: Internal error)`;
  const error = { message: 'Usage limit reached for 5 hour.', raw: err, err };

  it("replaces the agent's [error] line with one error card before the footer", () => {
    const items = buildTranscript([line('a', 'user', 'go'), line('b', 'assistant', `[error] ${quota}`)], [], { sentence: 'Rate limited' }, error);
    expect(items.map((i) => i.type)).toEqual(['user', 'error', 'footer']);
    expect(items[1]).toEqual({ type: 'error', key: 'error', message: 'Usage limit reached for 5 hour.', raw: err });
  });

  it('keeps the agent text that came before the [error] line', () => {
    const items = buildTranscript([line('b', 'assistant', `Working.\n[error] ${quota}`)], [], null, error);
    expect(items[0]).toEqual({ type: 'assistant', key: 'b', text: 'Working.' });
  });
});
