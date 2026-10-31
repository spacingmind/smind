// Shared fixtures for the hosts tests: wire-shaped fleet.host.* payloads.

import { FleetHost } from '../hosts/types';

export { fakeConnection } from './pluginFixtures';

export function host(over: Partial<FleetHost> = {}): FleetHost {
  return {
    hostId: 'h1',
    alias: 'mac',
    label: 'Mac',
    state: 'approved',
    pendingApproval: false,
    link: 'active',
    outboundEnabled: true,
    inbound: { enabled: false },
    createdAt: '2026-10-31T10:00:00Z',
    ...over,
  };
}
