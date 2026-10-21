// taskRowView.test.ts: every status the store can send x attention
// true/false, so no status renders an untested slot/word/badge.

import { describe, expect, it } from 'vitest';
import { taskRowView } from '../taskRowView';

describe('taskRowView', () => {
  it.each(['created', 'done', 'stopped'])('%s without attention: idle dot, capitalised word, no badge', (status) => {
    expect(taskRowView(status, false)).toEqual({ slot: { dot: 'idle' }, word: status[0].toUpperCase() + status.slice(1), wordTone: 'default', badge: null });
  });

  it.each(['created', 'done', 'stopped'])('%s with attention: unread dot + waiting badge', (status) => {
    expect(taskRowView(status, true)).toEqual({ slot: { dot: 'unread' }, word: status[0].toUpperCase() + status.slice(1), wordTone: 'default', badge: 'waiting' });
  });

  it('running keeps the spinner even with attention, warning word, badge only when attention', () => {
    expect(taskRowView('running', false)).toEqual({ slot: 'spinner', word: 'Running', wordTone: 'warning', badge: null });
    expect(taskRowView('running', true)).toEqual({ slot: 'spinner', word: 'Running', wordTone: 'warning', badge: 'waiting' });
  });

  it('error: danger dot and word without attention; unread dot + badge with', () => {
    expect(taskRowView('error', false)).toEqual({ slot: { dot: 'danger' }, word: 'Error', wordTone: 'danger', badge: null });
    expect(taskRowView('error', true)).toEqual({ slot: { dot: 'unread' }, word: 'Error', wordTone: 'danger', badge: 'waiting' });
  });

  it('an unknown status renders the raw status capitalised, idle dot', () => {
    expect(taskRowView('weird_state', false)).toEqual({ slot: { dot: 'idle' }, word: 'Weird_state', wordTone: 'default', badge: null });
  });
});
