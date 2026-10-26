// Shared fixtures for the inbox tests: wire-shaped inbox.* items.

import { InboxItem } from '../inbox/types';

export { fakeConnection } from './pluginFixtures';

export function inboxItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    key: 'verdict:1',
    kind: 'verdict',
    taskId: 7,
    title: 'Fix login',
    summary: 'Validator passed',
    createdAt: '2026-10-27T10:00:00Z',
    state: 'new',
    attention: 'normal',
    links: { taskId: 7, validationId: 1 },
    ...over,
  };
}
