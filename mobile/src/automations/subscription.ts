// subscription.ts: the automation.* events the screens listen to. Payloads carry ids and short reasons only; every
// event just means "read again", so the callback gets the topic and the automation id (0 when absent).

import { RelayConnection, RelayEvent } from '../relay/RelayConnection';
import { AUTOMATION_TOPICS } from './types';

export function watchAutomationEvents(conn: RelayConnection, onEvent: (e: { topic: string; automationId: number }) => void): () => void {
  return conn.subscribe(AUTOMATION_TOPICS, (event: RelayEvent) => {
    const p = event.payload as { automationId?: unknown } | null;
    onEvent({ topic: event.topic, automationId: typeof p?.automationId === 'number' ? p.automationId : 0 });
  });
}
