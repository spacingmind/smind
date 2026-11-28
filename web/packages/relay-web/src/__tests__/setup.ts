import { installRandomSource } from '@smind/relay-client/src/random';

installRandomSource((b) => crypto.getRandomValues(b));
