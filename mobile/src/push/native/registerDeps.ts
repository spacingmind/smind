import { Platform } from 'react-native';
import { loadPlatformIdentity } from '../../relay/platformKeyStore';
import { randomBytes } from '../../relay/random';
import type { RelayConnection } from '../../relay/RelayConnection';
import type { RegisterDeps } from '../register';
import { ensureAttentionChannel, getExpoPushToken, requestNotificationPermission } from './adapters';
import { platformPushKeyStore } from './platformPushKeys';

export async function buildRegisterDeps(conn: RelayConnection): Promise<RegisterDeps> {
  return {
    platform: Platform.OS === 'ios' ? 'ios' : 'android',
    deviceId: (await loadPlatformIdentity()).deviceId,
    ensureChannel: ensureAttentionChannel,
    requestPermission: requestNotificationPermission,
    getToken: getExpoPushToken,
    call: (m, p) => conn.call(m, p),
    keys: platformPushKeyStore(),
    randomBytes,
  };
}
