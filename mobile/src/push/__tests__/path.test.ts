import { describe, expect, it } from 'vitest';
import { openTaskIdFromPath } from '../path';

describe('openTaskIdFromPath', () => {
  it('extracts the open task id from a pathname', () => {
    expect(openTaskIdFromPath('/task/12')).toBe(12);
    expect(openTaskIdFromPath('/task/12/')).toBe(12);
    for (const p of ['/task/abc', '/task/0', '/task/1/x', '/task/', '/task/-3', '/task/99999999999999999999', '/', '', undefined, null]) {
      expect(openTaskIdFromPath(p), String(p)).toBeNull();
    }
  });
});
