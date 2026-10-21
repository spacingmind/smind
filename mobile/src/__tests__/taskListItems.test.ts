// taskListItems.test.ts covers the flattening: headers, grouped-card
// positions (only/first/middle/last), attention flags and per-section
// empties. Written against buildTaskSections' own output shape.

import { describe, expect, it } from 'vitest';
import { Space, Task } from '../api';
import { buildTaskSections } from '../taskSections';
import { taskListItems } from '../taskListItems';

function task(id: number, spaceId: number | null, status = 'created'): Task {
  return { ID: id, WorkspaceID: 1, SpaceID: spaceId, Title: `t${id}`, Status: status, CreatedAt: '', UpdatedAt: '' };
}

const space: Space = { ID: 7, WorkspaceID: 1, Title: 'bugs', CreatedAt: '', UpdatedAt: '' };

describe('taskListItems (flattening)', () => {
  it('no sections yields [] (the list-level empty state stays reachable)', () => {
    expect(taskListItems(buildTaskSections([], [], new Set()), new Set())).toEqual([]);
  });

  it('one task in a section yields header + a row with position "only"', () => {
    const items = taskListItems(buildTaskSections([space], [task(1, 7)], new Set()), new Set());
    expect(items).toEqual([
      { key: 'header-space-7', type: 'header', title: 'bugs' },
      { key: 'task-1', type: 'row', taskId: 1, title: 't1', status: 'created', attention: false, position: 'only' },
    ]);
  });

  it('three tasks yield first/middle/last', () => {
    const items = taskListItems(buildTaskSections([space], [task(1, 7), task(2, 7), task(3, 7)], new Set()), new Set());
    expect(items.filter((i) => i.type === 'row').map((i) => (i as { position: string }).position)).toEqual(['first', 'middle', 'last']);
  });

  it('two tasks yield first/last (never middle)', () => {
    const items = taskListItems(buildTaskSections([space], [task(1, 7), task(2, 7)], new Set()), new Set());
    expect(items.filter((i) => i.type === 'row').map((i) => (i as { position: string }).position)).toEqual(['first', 'last']);
  });

  it('an empty section yields a header + an empty item, not zero items', () => {
    const items = taskListItems(buildTaskSections([space], [], new Set()), new Set());
    expect(items).toEqual([
      { key: 'header-space-7', type: 'header', title: 'bugs' },
      { key: 'empty-space-7', type: 'empty', text: 'No tasks' },
    ]);
  });

  it('the ungrouped section is titled "Ungrouped" and attention is carried per task', () => {
    const items = taskListItems(buildTaskSections([space], [task(1, null), task(2, null, 'running')], new Set([2])), new Set([2]));
    expect(items[0]).toEqual({ key: 'header-ungrouped', type: 'header', title: 'Ungrouped' });
    expect(items[1]).toMatchObject({ taskId: 1, attention: false });
    expect(items[2]).toMatchObject({ taskId: 2, attention: true, status: 'running' });
  });

  it('keys are unique across sections and stable', () => {
    const items = taskListItems(
      buildTaskSections([space], [task(1, null), task(1, 7)], new Set()), // same id in two sections can't happen; keys still unique per section prefix
      new Set(),
    );
    const keys = items.map((i) => i.key);
    expect(new Set(keys).size).toBe(keys.length);
  });
});
