// The new-task route (M6): a guarded modal over the app group -- the form
// needs a live connection (like app/index.tsx, return null without one).

import { View } from 'react-native';
import { Stack, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { NewTaskScreen } from '../src/screens/NewTaskScreen';
import { TextButton } from '../src/ui/TextButton';

export default function NewTaskRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen
        options={{
          title: 'New task',
          headerLeft: () => (
            <View style={{ justifyContent: 'center', alignSelf: 'stretch' }}>
              <TextButton label="Cancel" onPress={() => router.back()} />
            </View>
          ),
        }}
      />
      <NewTaskScreen />
    </>
  );
}
