// ConnectionProvider.tsx owns the app session's one live RelayConnection
// (ADR-0036 decision 2): the pairing screen hands it over via connect(), and
// this provider is the only code that closes it or resets the device identity
// for the session. Rules that can be pure live in connectionActions.ts.
//
// No root <Host> here: each @expo/ui subtree carries its own Host via
// src/ui/UiHost (see UiHost.tsx's header).

import { createContext, ReactNode, useCallback, useContext, useMemo, useRef, useState } from 'react';
import { dropConnection, performResetIdentity, replaceConnection } from './connectionActions';
import { ConnectionState, RelayConnection } from '../relay/RelayConnection';
import { resetDeviceIdentity } from '../relay/platformKeyStore';
import { useConnectionState } from '../relay/useConnectionState';

export interface ConnectionContextValue {
  conn: RelayConnection | null;
  state: ConnectionState;
  connect: (conn: RelayConnection) => void;
  disconnect: () => void;
  /** Forgets the device identity, then disconnects; stays connected if the identity cannot be removed. */
  resetIdentity: () => Promise<void>;
}

const ConnectionContext = createContext<ConnectionContextValue | null>(null);

export function ConnectionProvider({ children }: { children: ReactNode }) {
  const [conn, setConn] = useState<RelayConnection | null>(null);
  // The ref is the source of truth for what to close, so a stale callback (captured before `conn` changed)
  // can never close one connection and null another.
  const connRef = useRef<RelayConnection | null>(null);
  const { state } = useConnectionState(conn);

  const connect = useCallback((c: RelayConnection) => {
    connRef.current = replaceConnection(connRef.current, c);
    setConn(c);
  }, []);

  const disconnect = useCallback(() => {
    connRef.current = dropConnection(connRef.current);
    setConn(null);
  }, []);

  const resetIdentity = useCallback(async () => {
    await performResetIdentity(resetDeviceIdentity, disconnect);
  }, [disconnect]);

  const value = useMemo(() => ({ conn, state, connect, disconnect, resetIdentity }), [conn, state, connect, disconnect, resetIdentity]);
  return <ConnectionContext.Provider value={value}>{children}</ConnectionContext.Provider>;
}

/** The connection context, or null outside the provider. A route can render one frame after a guard flip, so routes use this and return null without a connection. */
export function useConnectionOrNull(): ConnectionContextValue | null {
  return useContext(ConnectionContext);
}

export function useConnection(): ConnectionContextValue {
  const value = useContext(ConnectionContext);
  if (!value) throw new Error('useConnection must be used inside ConnectionProvider');
  return value;
}
