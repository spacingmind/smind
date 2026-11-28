// store.ts keeps the device key and the pairing in IndexedDB (never
// localStorage/sessionStorage). Writes resolve only after the transaction's
// complete event.

export interface StoredIdentity {
  privateKey: CryptoKey;
  publicKey: Uint8Array;
  createdAt: number;
}

export interface StoredPairing {
  offerUrl: string;
  daemonId: string;
  relay: string;
  relayFingerprint: string;
  label: string;
  pairedAt: number;
}

export interface RelayStore {
  loadIdentity(): Promise<StoredIdentity | null>;
  saveIdentity(i: StoredIdentity): Promise<void>;
  deleteIdentity(): Promise<void>;
  loadPairing(): Promise<StoredPairing | null>;
  savePairing(p: StoredPairing): Promise<void>;
  deletePairing(): Promise<void>;
  close(): void;
}

const DB_NAME = 'smind-relay';
const DB_VERSION = 1;
const IDENTITY = 'identity';
const PAIRINGS = 'pairings';
const IDENTITY_KEY = 'device';
const PAIRING_KEY = 'current';

function req<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

export async function openRelayStore(
  factory: IDBFactory = indexedDB,
): Promise<RelayStore> {
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const open = factory.open(DB_NAME, DB_VERSION);
    open.onupgradeneeded = () => {
      const d = open.result;
      if (!d.objectStoreNames.contains(IDENTITY)) d.createObjectStore(IDENTITY);
      if (!d.objectStoreNames.contains(PAIRINGS)) d.createObjectStore(PAIRINGS);
    };
    open.onsuccess = () => resolve(open.result);
    open.onerror = () => reject(open.error);
    open.onblocked = () => reject(new Error('relay store open blocked'));
  });

  const read = async <T>(store: string, key: string): Promise<T | null> => {
    const tx = db.transaction(store, 'readonly');
    const v = await req(tx.objectStore(store).get(key));
    return (v as T | undefined) ?? null;
  };

  const write = (store: string, fn: (s: IDBObjectStore) => void): Promise<void> =>
    new Promise((resolve, reject) => {
      const tx = db.transaction(store, 'readwrite');
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error ?? new Error('transaction aborted'));
      fn(tx.objectStore(store));
    });

  return {
    loadIdentity: () => read<StoredIdentity>(IDENTITY, IDENTITY_KEY),
    saveIdentity: (i) => write(IDENTITY, (s) => void s.put(i, IDENTITY_KEY)),
    deleteIdentity: () => write(IDENTITY, (s) => void s.delete(IDENTITY_KEY)),
    loadPairing: () => read<StoredPairing>(PAIRINGS, PAIRING_KEY),
    savePairing: (p) => write(PAIRINGS, (s) => void s.put(p, PAIRING_KEY)),
    deletePairing: () => write(PAIRINGS, (s) => void s.delete(PAIRING_KEY)),
    close: () => db.close(),
  };
}
