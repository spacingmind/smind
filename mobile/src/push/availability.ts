// Whether this build can receive push at all (ADR-0040 sections 6 and 7). Pure:
// the native adapter supplies the platform and execution environment.

export type PushAvailability = 'available' | 'web' | 'expo_go' | 'ios_blocked';

export function pushAvailability(env: { platform: string; executionEnvironment: string | undefined }): PushAvailability {
  if (env.platform === 'web') return 'web';
  // Expo Go (ExecutionEnvironment.StoreClient) cannot receive remote push.
  if (env.executionEnvironment === 'storeClient') return 'expo_go';
  // ADR-0040 H2: iOS remote push needs the paid Apple program and is not shipped yet.
  if (env.platform === 'ios') return 'ios_blocked';
  if (env.platform === 'android') return 'available';
  return 'web';
}
