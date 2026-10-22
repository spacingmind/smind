import { describe, expect, it } from 'vitest';
import { NEW_TASK_HREF } from '../navigation/routes';

describe('NEW_TASK_HREF', () => {
  it('points at the new-task route', () => {
    expect(NEW_TASK_HREF).toBe('/new-task');
  });
});
