import { describe, expect, it } from 'vitest';
import { Space } from '../api';
import { issueMessage, spacesFor } from '../newTaskForm';

function space(id: number, workspaceId: number): Space {
  return { ID: id, WorkspaceID: workspaceId, Title: `Space ${id}`, CreatedAt: '', UpdatedAt: '' };
}

describe('spacesFor', () => {
  it('keeps only the workspace\'s spaces, in order', () => {
    const all = [space(1, 10), space(2, 20), space(3, 10)];
    expect(spacesFor(all, 10).map((s) => s.ID)).toEqual([1, 3]);
    expect(spacesFor(all, 99)).toEqual([]);
  });
});

describe('issueMessage', () => {
  it('gives each issue a sentence', () => {
    expect(issueMessage('title-empty')).toBe('Enter a title for the task.');
    expect(issueMessage('workspace-missing')).toBe('No workspace to create the task in.');
    expect(issueMessage('provider-missing')).toBe('Pick a provider for the first prompt.');
  });
});
