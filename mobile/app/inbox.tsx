// The Inbox (ADR-0045 section 5): a guarded route in the app group, reached from the Tasks header.

import { Stack, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { HOSTS_HREF, POLICY_HREF, reviewHref, taskHref } from '../src/navigation/routes';
import { InboxScreen } from '../src/screens/InboxScreen';

export default function InboxRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'Inbox' }} />
      <InboxScreen
        conn={ctx.conn}
        onOpenTask={(id, title) => router.push(taskHref({ ID: id, Title: title }))}
        onOpenHosts={(hostId) => router.push({ pathname: HOSTS_HREF, params: hostId ? { focus: hostId } : {} })}
        onOpenReview={(id, taskId) => router.push(reviewHref(id, taskId))}
        onOpenPolicy={() => router.push(POLICY_HREF)}
      />
    </>
  );
}
