// runTimeline.test.ts covers Item 3's Test Scenarios at the logic layer:
// a completed run's full historical transcript renders from run.logs
// events; live run.attach events render as they arrive; unknown event
// kinds are dropped rather than crashing; and (in the accompanying
// RelayConnection cancel test) detaching leaves no handler behind.

import { describe, expect, it } from 'vitest';
import { lineFromAttachEvent, timelineFromLogs } from '../runTimeline';

describe('timelineFromLogs (run.logs history)', () => {
  it('renders a completed run: user message, chunks, tool call, done', () => {
    const lines = timelineFromLogs({
      events: [
        { type: 'user_message', text: 'fix the flaky test' },
        { type: 'thinking', text: 'checking the retry loop' },
        { type: 'chunk', text: 'Looking at retry_test.go...' },
        { type: 'tool_call', toolName: 'read_file', title: 'retry_test.go', status: 'pending' },
        { type: 'tool_call', toolName: 'read_file', title: 'retry_test.go', status: 'completed' },
        { type: 'chunk', text: 'The backoff resets too early.' },
        { type: 'done', stopReason: 'end_turn' },
      ],
    });
    expect(lines).toEqual([
      { key: 'e0', role: 'user', text: 'fix the flaky test' },
      { key: 'e1', role: 'assistant (thinking)', text: 'checking the retry loop' },
      { key: 'e2', role: 'assistant', text: 'Looking at retry_test.go...' },
      { key: 'e3', role: '', text: 'tool: read_file — retry_test.go [pending]' },
      { key: 'e4', role: '', text: 'tool: read_file — retry_test.go [completed]' },
      { key: 'e5', role: 'assistant', text: 'The backoff resets too early.' },
      { key: 'e6', role: '', text: 'done (end_turn)' },
    ]);
  });

  it('an empty events list renders an empty timeline (no crash)', () => {
    expect(timelineFromLogs({})).toEqual([]);
    expect(timelineFromLogs({ events: [] })).toEqual([]);
  });

  it('drops unknown event types instead of rendering garbage', () => {
    expect(timelineFromLogs({ events: [{ type: 'raw' }] })).toEqual([]);
  });
});

describe('lineFromAttachEvent (run.attach live stream)', () => {
  it('maps each streamed event name to the same rendering as its logs twin', () => {
    expect(lineFromAttachEvent('chunk', { text: 'hi' }, 1)).toEqual([{ key: 'e1', role: 'assistant', text: 'hi' }]);
    expect(lineFromAttachEvent('user_message', { text: 'go' }, 2)).toEqual([{ key: 'e2', role: 'user', text: 'go' }]);
    expect(lineFromAttachEvent('thinking', { text: 'hm' }, 3)).toEqual([
      { key: 'e3', role: 'assistant (thinking)', text: 'hm' },
    ]);
    expect(lineFromAttachEvent('tool_call', { toolName: 'bash', status: 'running' }, 4)).toEqual([
      { key: 'e4', role: '', text: 'tool: bash [running]' },
    ]);
    expect(lineFromAttachEvent('done', { stopReason: 'end_turn' }, 5)).toEqual([
      { key: 'e5', role: '', text: 'done (end_turn)' },
    ]);
  });

  it('unknown live event names render nothing', () => {
    expect(lineFromAttachEvent('something_new', {}, 6)).toEqual([]);
  });
});
