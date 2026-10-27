// subscription.ts: the one inbox event. inbox.changed has an empty payload and only means "read again".

import { RelayConnection } from '../relay/RelayConnection';
import { INBOX_CHANGED_TOPIC } from './types';

export function watchInboxChanged(conn: RelayConnection, onChanged: () => void): () => void {
  return conn.subscribe([INBOX_CHANGED_TOPIC], () => onChanged());
}
