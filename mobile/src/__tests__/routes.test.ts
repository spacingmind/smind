import { describe, expect, it } from 'vitest';
import { parseTaskParams, taskHref } from '../navigation/routes';

describe('taskHref', () => {
  it('stringifies the id and carries the title', () => {
    expect(taskHref({ ID: 7, Title: 'Fix bug' })).toEqual({ pathname: '/task/[id]', params: { id: '7', title: 'Fix bug' } });
  });
});

describe('parseTaskParams', () => {
  it('parses a numeric id with a title', () => {
    expect(parseTaskParams({ id: '12', title: 'Fix' })).toEqual({ ok: true, id: 12, title: 'Fix' });
  });
  it('falls back to "Task <id>" for a missing or empty title', () => {
    expect(parseTaskParams({ id: '12' })).toEqual({ ok: true, id: 12, title: 'Task 12' });
    expect(parseTaskParams({ id: '12', title: '' })).toEqual({ ok: true, id: 12, title: 'Task 12' });
  });
  it('rejects non-positive-integer ids', () => {
    for (const id of ['abc', '0', '12abc', '-3', '1.5', '']) expect(parseTaskParams({ id })).toEqual({ ok: false });
  });
  it('rejects ids that are not plain ASCII digits or are too large to be exact', () => {
    for (const id of ['99999999999999999999', ' 5', '5 ', '+5', '٥']) expect(parseTaskParams({ id })).toEqual({ ok: false });
  });
  it('accepts leading zeros as the same id', () => {
    expect(parseTaskParams({ id: '007' })).toEqual({ ok: true, id: 7, title: 'Task 7' });
  });
  it('rejects empty array params', () => {
    expect(parseTaskParams({ id: [] })).toEqual({ ok: false });
    expect(parseTaskParams({ id: [''] })).toEqual({ ok: false });
  });
  it('rejects a missing id', () => {
    expect(parseTaskParams({})).toEqual({ ok: false });
    expect(parseTaskParams({ title: 'x' })).toEqual({ ok: false });
  });
  it('takes the first element of array params', () => {
    expect(parseTaskParams({ id: ['5', '6'], title: ['A', 'B'] })).toEqual({ ok: true, id: 5, title: 'A' });
  });
});
