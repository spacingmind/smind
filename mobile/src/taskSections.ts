// taskSections.ts builds TasksScreen's FlatList data. The web smoke test
// (mobile-web-smoke-test plan, Item 2) found the empty state never
// rendered because the ungrouped section was unconditional -- the data
// array was never empty, so ListEmptyComponent was unreachable. Sections
// are emitted only when they have content; an empty array is the exact
// data shape that makes FlatList render the list-level empty state.

import { Space, Task } from './api';
import { sortTasksByAttention } from './taskAttention';

export type TaskSection =
  | { kind: 'ungrouped'; tasks: Task[] }
  | { kind: 'grouped'; space: Space; tasks: Task[] };

export function buildTaskSections(spaces: Space[], tasks: Task[], pendingTaskIds: ReadonlySet<number>): TaskSection[] {
  const ungrouped = tasks.filter((t) => t.SpaceID === null);
  const sections: TaskSection[] = [];
  if (ungrouped.length > 0) {
    sections.push({ kind: 'ungrouped', tasks: sortTasksByAttention(ungrouped, pendingTaskIds) });
  }
  for (const space of spaces) {
    sections.push({
      kind: 'grouped',
      space,
      tasks: sortTasksByAttention(tasks.filter((t) => t.SpaceID === space.ID), pendingTaskIds),
    });
  }
  return sections;
}
