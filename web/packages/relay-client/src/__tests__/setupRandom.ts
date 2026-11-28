// vitest setup: the relay client has no implicit global CSPRNG (src/relay/random.ts),
// so node-side tests install node's.
/// <reference types="node" />
import { randomFillSync } from 'node:crypto';
import { installRandomSource } from '../random';

installRandomSource((bytes) => {
  randomFillSync(bytes);
});
