// taskListItems.ts flattens buildTaskSections' output into ONE item array
// for a virtualized FlatList (vercel-react-native-skills: the task list
// stays a FlatList; @expo/ui List is not virtualized). Pure and unit-tested
// so the item shape (key/type/taskId/position) never depends on rendering.

import { TaskSection } from './taskSections';

export type TaskRowPosition = 'only' | 'first' | 'middle' | 'last';

export type TaskListItem =
  | { key: string; type: 'header'; title: string }
  | { key: string; type: 'row'; taskId: number; title: string; status: string; attention: boolean; position: TaskRowPosition }
  | { key: string; type: 'empty'; text: string };

/** One header per section ('Ungrouped' or the space title), rows tagged with their grouped-card position, 'No tasks' for an empty section. */
export function taskListItems(sections: TaskSection[], pendingTaskIds: ReadonlySet<number>): TaskListItem[] {
  const items: TaskListItem[] = [];
  for (const section of sections) {
    const title = section.kind === 'ungrouped' ? 'Ungrouped' : section.space.Title;
    const key = section.kind === 'ungrouped' ? 'ungrouped' : `space-${section.space.ID}`;
    items.push({ key: `header-${key}`, type: 'header', title });
    if (section.tasks.length === 0) {
      items.push({ key: `empty-${key}`, type: 'empty', text: 'No tasks' });
      continue;
    }
    section.tasks.forEach((task, i) => {
      const position: TaskRowPosition =
        section.tasks.length === 1 ? 'only' : i === 0 ? 'first' : i === section.tasks.length - 1 ? 'last' : 'middle';
      items.push({
        key: `task-${task.ID}`,
        type: 'row',
        taskId: task.ID,
        title: task.Title,
        status: task.Status,
        attention: pendingTaskIds.has(task.ID),
        position,
      });
    });
  }
  return items;
}
