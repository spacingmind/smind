// newTaskForm.ts is M6's pure form-state helpers: everything the
// NewTaskScreen would otherwise inline (issue sentences, space shaping).
// No react-native import -- vitest covers it as plain data.

import { Space } from './api';
import { NewTaskIssue } from './newTask';

/** The spaces of one workspace, for the Space picker. */
export function spacesFor(spaces: Space[], workspaceId: number): Space[] {
  return spaces.filter((s) => s.WorkspaceID === workspaceId);
}

/** The user-readable sentence for a validateNewTask issue. */
export function issueMessage(issue: NewTaskIssue): string {
  switch (issue) {
    case 'title-empty':
      return 'Enter a title for the task.';
    case 'workspace-missing':
      return 'No workspace to create the task in.';
    case 'provider-missing':
      return 'Pick a provider for the first prompt.';
  }
}
