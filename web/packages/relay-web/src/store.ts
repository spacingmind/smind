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

/** A load that tells "nothing stored" from "something stored that is not valid". */
export type Inspected<T> = { status: 'absent' } | { status: 'invalid' } | { status: 'ok'; value: T };

export interface RelayStore {
  loadIdentity(): Promise<StoredIdentity | null>;
  saveIdentity(i: StoredIdentity): Promise<void>;
  deleteIdentity(): Promise<void>;
  loadPairing(): Promise<StoredPairing | null>;
  savePairing(p: StoredPairing): Promise<void>;
  deletePairing(): Promise<void>;
  /** Like loadIdentity, but a stored record that fails validation is 'invalid', not null. */
  inspectIdentity(): Promise<Inspected<StoredIdentity>>;
  inspectPairing(): Promise<Inspected<StoredPairing>>;
  /** Writes the identity and the pairing in ONE transaction: both or neither. */
  savePaired(identity: StoredIdentity, pairing: StoredPairing): Promise<void>;
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

const BLOCKED_MS = 5_000;

/**
 * Opens the database. A blocked open (another tab holds an old version) keeps
 * waiting for up to blockedMs and then rejects; a database that opens after
 * that is closed at once so it cannot block anyone in turn.
 */
export function openDatabase(
  factory: IDBFactory,
  upgrade: (db: IDBDatabase) => void,
  blockedMs: number = BLOCKED_MS,
): Promise<IDBDatabase> {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const open = factory.open(DB_NAME, DB_VERSION);
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const fail = (e: unknown) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(e);
    };
    open.onupgradeneeded = () => upgrade(open.result);
    open.onsuccess = () => {
      if (settled) {
        open.result.close(); // it opened after we gave up
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve(open.result);
    };
    open.onerror = () => fail(open.error);
    open.onblocked = () => {
      timer ??= setTimeout(
        () => fail(new Error('the relay store is blocked by another tab: close the other smind tabs and reload')),
        blockedMs,
      );
    };
  });
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const isStr = (v: unknown): v is string => typeof v === 'string';
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

function validIdentity(v: unknown): v is StoredIdentity {
  if (!isObj(v)) return false;
  const k = v.privateKey;
  return (
    isObj(k) &&
    k.type === 'private' &&
    isObj(k.algorithm) &&
    k.algorithm.name === 'Ed25519' &&
    k.extractable === false &&
    v.publicKey instanceof Uint8Array &&
    v.publicKey.length === 32 &&
    isNum(v.createdAt)
  );
}

function validPairing(v: unknown): v is StoredPairing {
  return (
    isObj(v) &&
    isStr(v.offerUrl) &&
    isStr(v.daemonId) &&
    isStr(v.relay) &&
    isStr(v.relayFingerprint) &&
    isStr(v.label) &&
    isNum(v.pairedAt)
  );
}

export async function openRelayStore(
  factory: IDBFactory = indexedDB,
): Promise<RelayStore> {
  const db = await openDatabase(factory, (d) => {
    if (!d.objectStoreNames.contains(IDENTITY)) d.createObjectStore(IDENTITY);
    if (!d.objectStoreNames.contains(PAIRINGS)) d.createObjectStore(PAIRINGS);
  });
  let closed = false;
  const closedError = () => new Error('the relay store is closed');
  db.onversionchange = () => {
    closed = true;
    db.close();
  };
  db.onclose = () => {
    closed = true;
  };
  const begin = (stores: string | string[], mode: IDBTransactionMode): IDBTransaction => {
    if (closed) throw closedError();
    try {
      return db.transaction(stores, mode);
    } catch {
      closed = true;
      throw closedError();
    }
  };

  const read = async (store: string, key: string): Promise<unknown> => {
    const tx = begin(store, 'readonly');
    const v = await req(tx.objectStore(store).get(key));
    return v ?? null;
  };

  const inspect = async <T>(store: string, key: string, valid: (v: unknown) => v is T): Promise<Inspected<T>> => {
    const raw = await read(store, key);
    if (raw === null) return { status: 'absent' };
    return valid(raw) ? { status: 'ok', value: raw } : { status: 'invalid' };
  };
  const value = async <T>(i: Promise<Inspected<T>>): Promise<T | null> => {
    const r = await i;
    return r.status === 'ok' ? r.value : null;
  };

  const write = (stores: string | string[], fn: (tx: IDBTransaction) => void): Promise<void> =>
    new Promise((resolve, reject) => {
      let tx: IDBTransaction;
      try {
        tx = begin(stores, 'readwrite');
      } catch (e) {
        reject(e);
        return;
      }
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error ?? new Error('transaction aborted'));
      try {
        fn(tx);
      } catch (e) {
        // A request that throws (e.g. a value that cannot be cloned) must not
        // leave an earlier put in the same transaction to commit.
        try {
          tx.abort();
        } catch {
          // already finished
        }
        reject(e);
      }
    });

  return {
    inspectIdentity: () => inspect(IDENTITY, IDENTITY_KEY, validIdentity),
    inspectPairing: () => inspect(PAIRINGS, PAIRING_KEY, validPairing),
    loadIdentity: () => value(inspect(IDENTITY, IDENTITY_KEY, validIdentity)),
    saveIdentity: (i) => write(IDENTITY, (tx) => void tx.objectStore(IDENTITY).put(i, IDENTITY_KEY)),
    deleteIdentity: () => write(IDENTITY, (tx) => void tx.objectStore(IDENTITY).delete(IDENTITY_KEY)),
    loadPairing: () => value(inspect(PAIRINGS, PAIRING_KEY, validPairing)),
    savePairing: (p) => write(PAIRINGS, (tx) => void tx.objectStore(PAIRINGS).put(p, PAIRING_KEY)),
    deletePairing: () => write(PAIRINGS, (tx) => void tx.objectStore(PAIRINGS).delete(PAIRING_KEY)),
    savePaired: (i, p) =>
      write([IDENTITY, PAIRINGS], (tx) => {
        tx.objectStore(IDENTITY).put(i, IDENTITY_KEY);
        tx.objectStore(PAIRINGS).put(p, PAIRING_KEY);
      }),
    close: () => {
      closed = true;
      db.close();
    },
  };
}
