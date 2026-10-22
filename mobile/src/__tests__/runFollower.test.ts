import { describe, it, expect, vi } from 'vitest';
import { RunFollower, TERMINAL_STATUSES } from '../runFollower';

describe('RunFollower', () => {
  it('adopts an unknown running run of its chat once', () => {
    const onAdopt = vi.fn();
    const onLiveChange = vi.fn();
    const f = new RunFollower(1, onAdopt, onLiveChange);
    const e = { runId: 'r1', chatId: 1, status: 'running' };
    f.onRunStatus(e);
    f.onRunStatus(e);
    expect(onAdopt).toHaveBeenCalledTimes(1);
    expect(onAdopt).toHaveBeenCalledWith('r1');
    expect(onLiveChange).toHaveBeenCalledTimes(1);
    expect(f.liveRunId).toBe('r1');
  });

  it('ignores run.status for another chat', () => {
    const onAdopt = vi.fn();
    const onLiveChange = vi.fn();
    const f = new RunFollower(1, onAdopt, onLiveChange);
    f.onRunStatus({ runId: 'r1', chatId: 2, status: 'running' });
    expect(onAdopt).not.toHaveBeenCalled();
    expect(onLiveChange).not.toHaveBeenCalled();
    expect(f.liveRunId).toBeNull();
  });

  it('does not adopt a run it already knows (run.start racing run.status)', () => {
    const onAdopt = vi.fn();
    const onLiveChange = vi.fn();
    const f = new RunFollower(1, onAdopt, onLiveChange);
    f.markKnown('r1', 'running');
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'running' });
    expect(onAdopt).not.toHaveBeenCalled();
    expect(f.liveRunId).toBe('r1');
  });

  // The race behind the intermittent "delivered prompt has no user line": when the previous run ends, the screen refreshes
  // (markKnown with the daemon's own list) and can see the queue-delivered run before its run.status arrives. The follower then
  // never signals an adoption for that run, so the refresh's own attach is the only one -- it must carry the run's prompt itself.
  it('a refresh that finds a delivered run first suppresses its later adoption', () => {
    const onAdopt = vi.fn();
    const f = new RunFollower(1, onAdopt, vi.fn());
    f.markKnown('r1', 'running');
    f.markKnown('r1', 'stopped');
    f.markKnown('r2', 'running'); // refresh after r1 ended already lists r2
    f.onRunStatus({ runId: 'r2', chatId: 1, status: 'running' }); // the status event that would have adopted it
    expect(onAdopt).not.toHaveBeenCalled();
    expect(f.liveRunId).toBe('r2');
  });

  it('markKnown with running sets the live run without adopting', () => {
    const onAdopt = vi.fn();
    const onLiveChange = vi.fn();
    const f = new RunFollower(1, onAdopt, onLiveChange);
    f.markKnown('r1', 'running');
    expect(onAdopt).not.toHaveBeenCalled();
    expect(onLiveChange).toHaveBeenCalledWith('r1');
    expect(f.liveRunId).toBe('r1');
  });

  it('tracks the live run and clears it when that run ends', () => {
    const f = new RunFollower(1, vi.fn(), vi.fn());
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'running' });
    expect(f.liveRunId).toBe('r1');
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'done' });
    expect(f.liveRunId).toBeNull();
  });

  it('a late status for an older run does not clear the current live run', () => {
    const f = new RunFollower(1, vi.fn(), vi.fn());
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'running' });
    f.onRunStatus({ runId: 'r2', chatId: 1, status: 'running' });
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'done' });
    expect(f.liveRunId).toBe('r2');
  });

  it('a terminal status for an unknown run is not adopted and not remembered', () => {
    const onAdopt = vi.fn();
    const onLiveChange = vi.fn();
    const f = new RunFollower(1, onAdopt, onLiveChange);
    for (const status of TERMINAL_STATUSES) {
      f.onRunStatus({ runId: 'r1', chatId: 1, status });
    }
    expect(onAdopt).not.toHaveBeenCalled();
    expect(f.liveRunId).toBeNull();
    f.onRunStatus({ runId: 'r1', chatId: 1, status: 'running' });
    expect(onAdopt).toHaveBeenCalledWith('r1');
    expect(f.liveRunId).toBe('r1');
  });

  it('a new running run replaces the live one', () => {
    const f = new RunFollower(1, vi.fn(), vi.fn());
    f.onRunStatus({ runId: 'a', chatId: 1, status: 'running' });
    f.onRunStatus({ runId: 'b', chatId: 1, status: 'running' });
    expect(f.liveRunId).toBe('b');
    f.onRunStatus({ runId: 'a', chatId: 1, status: 'error' });
    expect(f.liveRunId).toBe('b');
  });
});
