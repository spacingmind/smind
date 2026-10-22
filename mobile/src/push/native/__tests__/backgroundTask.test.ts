import { readFileSync } from 'node:fs';
import path from 'node:path';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const vector = JSON.parse(
  readFileSync(path.resolve(__dirname, '../../../../../internal/push/testdata/push_seal_vector.json'), 'utf8'),
);

const mocks = vi.hoisted(() => ({
  os: 'android',
  defineTask: vi.fn(),
  registerTaskAsync: vi.fn(),
  scheduleNotificationAsync: vi.fn(),
  store: new Map<string, string>(),
}));

vi.mock('react-native', () => ({
  Platform: {
    get OS() {
      return mocks.os;
    },
  },
}));
vi.mock('expo-task-manager', () => ({ defineTask: mocks.defineTask }));
vi.mock('expo-notifications', () => ({
  registerTaskAsync: mocks.registerTaskAsync,
  scheduleNotificationAsync: mocks.scheduleNotificationAsync,
}));
vi.mock('expo-secure-store', () => ({
  AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY: 1,
  isAvailableAsync: async () => true,
  getItemAsync: async (k: string) => mocks.store.get(k) ?? null,
  setItemAsync: async (k: string, v: string) => void mocks.store.set(k, v),
  deleteItemAsync: async (k: string) => void mocks.store.delete(k),
}));

beforeEach(() => {
  vi.resetModules();
  mocks.os = 'android';
  mocks.defineTask.mockReset();
  mocks.registerTaskAsync.mockReset().mockResolvedValue(null);
  mocks.scheduleNotificationAsync.mockReset().mockResolvedValue('id');
  mocks.store.clear();
});

describe('backgroundTask', () => {
  it('defines and registers the push task exactly once on android', async () => {
    await import('../backgroundTask');
    expect(mocks.defineTask).toHaveBeenCalledTimes(1);
    expect(mocks.defineTask.mock.calls[0][0]).toBe('smind-push-notification');
    expect(mocks.registerTaskAsync).toHaveBeenCalledTimes(1);
    expect(mocks.registerTaskAsync).toHaveBeenCalledWith('smind-push-notification');
  });

  it('does nothing on ios and web', async () => {
    for (const os of ['ios', 'web']) {
      vi.resetModules();
      mocks.os = os;
      await import('../backgroundTask');
    }
    expect(mocks.defineTask).not.toHaveBeenCalled();
    expect(mocks.registerTaskAsync).not.toHaveBeenCalled();
  });

  it('the task handler presents a notification for a push it can open', async () => {
    mocks.store.set(`smind.push.key.v1.${vector.device_id}`, vector.key_hex);
    mocks.store.set('smind.push.keys.v1', JSON.stringify([vector.device_id]));
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime((vector.event.at + 5) * 1000);
    try {
      await import('../backgroundTask');
      const task = mocks.defineTask.mock.calls[0][1];
      await task({ data: { data: { dataString: JSON.stringify(vector.blob) } }, error: null });
    } finally {
      vi.useRealTimers();
    }
    expect(mocks.scheduleNotificationAsync).toHaveBeenCalledTimes(1);
    const arg = mocks.scheduleNotificationAsync.mock.calls[0][0];
    expect(arg.identifier).toBe('evt-0001');
    expect(arg.content.title).toBe(vector.event.title);
    expect(arg.trigger).toEqual({ channelId: 'attention' });
  });

  it('the task handler ignores an error argument', async () => {
    await import('../backgroundTask');
    const task = mocks.defineTask.mock.calls[0][1];
    await task({ data: null, error: {} });
    expect(mocks.scheduleNotificationAsync).not.toHaveBeenCalled();
  });
});
