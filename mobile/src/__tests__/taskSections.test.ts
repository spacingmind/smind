// tasksScreen.test.ts covers the FlatList data construction TasksScreen
// renders from (taskSections.ts -- taskAttention.ts's import-light
// discipline). The web smoke test (mobile-web-smoke-test plan, Item 2)
// found the empty state never rendered: the ungrouped section was
// unconditional, so the data array was never empty and FlatList's
// ListEmptyComponent ("No workspaces yet") was unreachable.

import { describe, expect, it } from 'vitest';
import { buildTaskSections } from '../taskSections';
import { Space, Task } from '../api';

function task(id: number, spaceId: number | null, status = 'created'): Task {
  return { ID: id, WorkspaceID: 1, SpaceID: spaceId, Title: `t${id}`, Status: status, CreatedAt: '', UpdatedAt: '' };
}

const space: Space = { ID: 7, WorkspaceID: 1, Title: 'bugs', CreatedAt: '', UpdatedAt: '' };

describe('buildTaskSections (empty state)', () => {
  it('no spaces and no tasks yields [], the exact FlatList data that renders ListEmptyComponent', () => {
    expect(buildTaskSections([], [], new Set())).toEqual([]);
  });

  it('spaces with no tasks still render as sections (per-section "No tasks"), never the list empty state', () => {
    expect(buildTaskSections([space], [], new Set())).toEqual([{ kind: 'grouped', space, tasks: [] }]);
  });
});

describe('buildTaskSections (sectioning + attention sort)', () => {
  it('ungrouped tasks come first, sorted pending before running before rest', () => {
    const sections = buildTaskSections([space], [task(1, 7), task(2, null, 'running'), task(3, null)], new Set([3]));
    expect(sections.map((s) => s.kind)).toEqual(['ungrouped', 'grouped']);
    expect(sections[0].tasks.map((t) => t.ID)).toEqual([3, 2]);
    expect(sections[1].tasks.map((t) => t.ID)).toEqual([1]);
  });

  it('the ungrouped section appears only when an ungrouped task exists', () => {
    expect(buildTaskSections([space], [task(1, 7)], new Set())).toEqual([
      { kind: 'grouped', space, tasks: [task(1, 7)] },
    ]);
  });

  it('tasks are grouped by SpaceID match, not merely order', () => {
    const sections = buildTaskSections([space], [task(1, 7), task(2, null), task(3, 7)], new Set());
    expect(sections[0].tasks.map((t) => t.ID)).toEqual([2]);
    expect(sections[1].tasks.map((t) => t.ID)).toEqual([1, 3]);
  });
});
