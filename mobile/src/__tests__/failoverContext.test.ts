import { describe, expect, it } from 'vitest';
import { ChatQueueItem, RunSummary } from '../api';
import { findHeldUntil, findSuccessor } from '../failoverContext';

const item = (over: Partial<ChatQueueItem>): ChatQueueItem =>
  ({ ID: 1, ChatID: 1, Prompt: 'p', Status: 'queued', NotBefore: '2026-10-23T10:35:00Z', ...over }) as ChatQueueItem;
const run = (over: Partial<RunSummary>): RunSummary => ({ ID: 'r2', Provider: 'glm', ChatID: 3, ...over }) as RunSummary;

describe('findHeldUntil', () => {
  it('returns the NotBefore of the queued item with the run prompt', () => {
    expect(findHeldUntil([item({})], 'p')?.toISOString()).toBe('2026-10-23T10:35:00.000Z');
  });
  it('ignores other prompts, delivered/cancelled items and items without a hold', () => {
    expect(findHeldUntil([item({ Prompt: 'x' }), item({ Status: 'delivered' }), item({ Status: 'cancelled' }), item({ NotBefore: null })], 'p')).toBeNull();
  });
  it('takes the earliest of several holds', () => {
    expect(findHeldUntil([item({ NotBefore: '2026-10-23T12:00:00Z' }), item({ NotBefore: '2026-10-23T11:00:00Z' })], 'p')?.toISOString()).toBe('2026-10-23T11:00:00.000Z');
  });
});

describe('findSuccessor', () => {
  it('finds the run whose FailoverOf is this run', () => {
    expect(findSuccessor([run({ FailoverOf: 'r1' })], 'r1')).toEqual({ provider: 'glm', chatId: 3 });
  });
  it('is null when nothing continues the run', () => {
    expect(findSuccessor([run({})], 'r1')).toBeNull();
  });
});
