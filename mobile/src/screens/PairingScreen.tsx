// PairingScreen.tsx is Milestone 1's proof-of-life screen, extracted from
// App.tsx for Milestone 2 (docs/plans/active/mobile-app-milestone-2.md
// Item 2): paste a pairing URL, Connect. The difference from Milestone 1
// is that the connection is NOT one-shot anymore -- on success the live
// RelayConnection is handed to the app's screen stack instead of closing
// it after one workspace.list.
//
// Rebuilt on the token system (mobile-ui-polish plan Item 1): every
// color comes from useAppTheme(), never a hardcoded hex literal.

import { useEffect, useMemo, useRef, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { UiHost } from '../ui/UiHost';
import { StatusBar } from 'expo-status-bar';
import { CERTIFICATE_HELP, describeState } from '../relay/connectionStatus';
import { defaultDeviceLabel, loadPlatformIdentity, resetDeviceIdentity } from '../relay/platformKeyStore';
import { ConnectionState, RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { ConnectionStatusCard } from './ConnectionStatusCard';

type Status = { kind: 'idle' } | { kind: 'starting' } | { kind: 'error'; message: string };

interface Props {
  onConnected: (conn: RelayConnection) => void;
}

export function PairingScreen({ onConnected }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [pairingUrl, setPairingUrl] = useState('');
  // @expo/ui's TextInput binds text via an ObservableState, not a plain
  // string prop (mobile-expo-ui-adoption plan Item 2); the React state
  // stays the source of truth for the Connect button's disabled gate.
  const pairingUrlState = useNativeState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  // The connection being set up. The relay connection keeps going while the
  // daemon has not approved this device (pending): the card below says so and
  // the screen moves on by itself once it is approved.
  const attempt = useRef<RelayConnection | null>(null);
  const stopTracking = useRef<(() => void) | null>(null);
  const handedOff = useRef(false);
  const starting = useRef(false);
  const [connState, setConnState] = useState<ConnectionState | null>(null);

  useEffect(
    () => () => {
      stopTracking.current?.();
      if (!handedOff.current) attempt.current?.close();
    },
    [],
  );

  function cancelAttempt() {
    stopTracking.current?.();
    stopTracking.current = null;
    attempt.current?.close();
    attempt.current = null;
    setConnState(null);
    setStatus({ kind: 'idle' });
  }

  async function handleConnect() {
    if (attempt.current || starting.current) return; // one attempt at a time: a second would fight the first over the same device
    starting.current = true;
    setStatus({ kind: 'starting' });
    try {
      const identity = await loadPlatformIdentity();
      const conn = RelayConnection.open(pairingUrl.trim(), { identity, label: defaultDeviceLabel() });
      attempt.current = conn;
      const track = (s: ConnectionState) => {
        setConnState(s);
        if (s.kind === 'failed' || s.kind === 'revoked') {
          // Over for good: let Connect (and Reset) start from a clean slate.
          conn.close();
          attempt.current = null;
        }
        if (s.kind === 'connected' && !handedOff.current) {
          handedOff.current = true;
          stopTracking.current?.();
          stopTracking.current = null;
          onConnected(conn);
        }
      };
      stopTracking.current = conn.onState(track);
      track(conn.state);
      setStatus({ kind: 'idle' });
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    } finally {
      starting.current = false;
    }
  }

  async function handleResetIdentity() {
    cancelAttempt();
    setStatus({ kind: 'starting' }); // Connect stays disabled until the old key is gone
    try {
      await resetDeviceIdentity();
      setStatus({ kind: 'idle' });
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  }

  const busy = status.kind === 'starting' || (connState !== null && connState.kind !== 'failed' && connState.kind !== 'revoked');
  const view = connState ? describeState(connState) : null;
  const actions =
    connState?.kind === 'pending'
      ? [{ label: 'Cancel', onPress: cancelAttempt }]
      : connState?.kind === 'revoked'
        ? [
            { label: 'Reset device identity', onPress: handleResetIdentity },
            { label: 'Back', onPress: cancelAttempt },
          ]
        : connState?.kind === 'failed'
          ? [{ label: 'Back', onPress: cancelAttempt }]
          : [];

  return (
    <View style={styles.container}>
      <StatusBar style="auto" />
      <Text style={styles.title}>smind pairing</Text>
      <Text style={styles.label}>Paste a pairing URL from `smind relay offer`:</Text>
      <UiHost style={styles.inputHost}>
        <ExpoTextInput
          style={styles.inputBox}
          textStyle={styles.inputText}
          multiline
          numberOfLines={3}
          autoCapitalize="none"
          autoCorrect={false}
          placeholder="https://spacingmind.sh/pair#offer=..."
          placeholderTextColor={theme.foregroundMuted}
          value={pairingUrlState}
          onChangeText={(text) => {
            pairingUrlState.value = text;
            setPairingUrl(text);
          }}
        />
      </UiHost>
      <View style={styles.connectWrap}>
        <UiHost>
          <Button
            label={busy ? 'Connecting…' : 'Connect'}
            onPress={handleConnect}
            disabled={!pairingUrl.trim() || busy}
          />
        </UiHost>
      </View>
      {view && <ConnectionStatusCard view={view} actions={actions} />}
      {status.kind === 'error' && <Text style={styles.error}>{status.message}</Text>}
      <Text style={styles.help}>{CERTIFICATE_HELP}</Text>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.surface[0],
      paddingTop: theme.spacing[16],
      paddingHorizontal: theme.spacing[4],
    },
    title: {
      fontSize: theme.type.sectionTitle.fontSize,
      lineHeight: theme.type.sectionTitle.lineHeight,
      fontWeight: theme.type.sectionTitle.fontWeight,
      color: theme.foreground,
      marginBottom: theme.spacing[4],
    },
    label: {
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      color: theme.foregroundMuted,
      marginBottom: theme.spacing[2],
    },
    inputHost: {
      alignSelf: 'stretch',
    },
    inputBox: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
    },
    inputText: {
      fontSize: theme.type.codeAnnotation.fontSize,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      color: theme.foreground,
    },
    connectWrap: {
      marginTop: theme.spacing[4],
    },
    help: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      color: theme.foregroundMuted,
      marginTop: theme.spacing[6],
    },
    error: {
      color: theme.status.danger,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      marginTop: theme.spacing[4],
    },
  });
}
