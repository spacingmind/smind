// One steward review, read-only (S3b): the inbox deep-links here with the
// review id (and its task id, when known). Resolve pops back to the inbox.

import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../../src/connection/ConnectionProvider';
import { parseReviewParams, taskHref } from '../../src/navigation/routes';
import { StewardReviewScreen } from '../../src/screens/StewardReviewScreen';
import { EmptyState } from '../../src/ui/EmptyState';

export default function ReviewRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  const params = useLocalSearchParams<{ id?: string; task?: string }>();
  if (!ctx?.conn) return null;
  const parsed = parseReviewParams(params);
  if (!parsed.ok) {
    return (
      <>
        <Stack.Screen options={{ title: 'Review' }} />
        <EmptyState title="Unknown review" body="This link does not name a review." />
      </>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: 'Review' }} />
      <StewardReviewScreen
        conn={ctx.conn}
        reviewId={parsed.id}
        onOpenTask={(id) => router.push(taskHref({ ID: id, Title: `Task ${id}` }))}
        onResolved={() => router.back()}
      />
    </>
  );
}
