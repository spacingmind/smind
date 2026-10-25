// Shared fixtures for the plugin tests: wire-shaped plugin.* payloads and a scripted RelayConnection that records
// every RPC (the api.test.ts pattern).

import { RelayConnection } from '../relay/RelayConnection';
import { PluginCommand, PluginConsent, PluginInfo, PluginListing } from '../plugins/types';

export function consent(over: Partial<PluginConsent> = {}): PluginConsent {
  return { id: 'acme', name: 'Acme', version: '1.2.0', source: '/plugins/acme', sourceKind: 'dir', dataOnly: true, ...over };
}

export function plugin(over: Partial<PluginInfo> & { id?: string } = {}): PluginInfo {
  const id = over.id ?? 'acme';
  return {
    id,
    name: id[0].toUpperCase() + id.slice(1),
    version: '1.2.0',
    source: `/plugins/${id}`,
    sourceKind: 'dir',
    enabled: true,
    active: true,
    consent: consent({ id, name: id }),
    ...over,
  };
}

export function listing(plugins: PluginInfo[], globalEnabled = true): PluginListing {
  return { globalEnabled, plugins };
}

export function cmd(pluginId: string, name: string, prompt = `Do ${name}.`, description = ''): PluginCommand {
  return { plugin: pluginId, name, qualified: `${pluginId}:${name}`, description, prompt };
}

const dummyOffer = { daemonId: '', publicKey: new Uint8Array(), relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' };

export function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}
