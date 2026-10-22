// PairingScreen.tsx is Milestone 1's proof-of-life screen, extracted from
// App.tsx for Milestone 2 (docs/plans/active/mobile-app-milestone-2.md
// Item 2): paste a pairing URL, Connect. The difference from Milestone 1
// is that the connection is NOT one-shot anymore -- on success the live
// RelayConnection is handed to the app's screen stack instead of closing
// it after one workspace.list.
//
// Rebuilt in the ZCode look (mobile-zcode-redesign M2): a keyboard-safe
// scroll view over theme.background, the form in the frozen Card, every
// color from useAppTheme(). A native @expo/ui FieldGroup was NOT used: the
// universal FieldGroup is a full-screen web ScrollView with its own
// hardcoded gray[50] background and padding (node_modules/@expo/ui/src/
// universal/FieldGroup/FieldGroup.tsx), which would break the theme
// background and the keyboard-safe scrolling here -- the documented
// fallback (Card + the same TextInput/Button) is used instead.

import { useEffect, useMemo, useRef, useState } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { UiHost } from '../ui/UiHost';
import { CERTIFICATE_HELP, describeState } from '../relay/connectionStatus';
import { defaultDeviceLabel, loadPlatformIdentity, resetDeviceIdentity } from '../relay/platformKeyStore';
import { ConnectionState, RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Card } from '../ui/Card';
import { ConnectionStatusCard } from './ConnectionStatusCard';
import { DaemonPasswordPrompt } from './DaemonPasswordPrompt';

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
          // Stop listening first, or close() would replace this card with 'closed'.
          stopTracking.current?.();
          stopTracking.current = null;
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
    <ScrollView
      style={styles.container}
      contentContainerStyle={styles.content}
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps="handled"
    >
      <Text style={styles.label}>Paste a pairing URL from `smind relay offer`:</Text>
      <Card style={styles.formCard}>
        <UiHost style={styles.inputHost}>
          <ExpoTextInput
            style={styles.inputBox}
            textStyle={styles.inputText}
            multiline
            numberOfLines={3}
            autoCapitalize="none"
            autoCorrect={false}
            placeholder="https://spacingmind.sh/pair#offer=..."
            placeholderTextColor={theme.foregroundSubtlest}
            value={pairingUrlState}
            onChangeText={(text) => {
              pairingUrlState.value = text;
              setPairingUrl(text);
            }}
          />
        </UiHost>
        <UiHost style={styles.connectHost}>
          <Button
            variant="filled"
            label={busy ? 'Connecting…' : 'Connect'}
            onPress={handleConnect}
            disabled={!pairingUrl.trim() || busy}
            style={{ height: 44 }}
          />
        </UiHost>
      </Card>
      {connState && (connState.kind === 'password' || connState.kind === 'locked') ? (
        <DaemonPasswordPrompt
          state={connState}
          onSubmit={(password) => attempt.current?.submitPassword(password)}
          actions={[{ label: 'Cancel', onPress: cancelAttempt }]}
        />
      ) : view ? (
        <ConnectionStatusCard view={view} actions={actions} />
      ) : null}
      {status.kind === 'error' && <Text style={styles.error}>{status.message}</Text>}
      <Text style={styles.help} selectable>
        {CERTIFICATE_HELP}
      </Text>
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], gap: theme.spacing[4] },
    label: {
      fontSize: theme.type.body.fontSize,
      lineHeight: theme.type.body.lineHeight,
      color: theme.foregroundSubtle,
    },
    formCard: { gap: theme.spacing[3] },
    inputHost: { alignSelf: 'stretch' },
    inputBox: {
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    inputText: {
      // Web: 16px keeps iOS Safari from zooming on focus (expo-native-ui input floor).
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize,
      fontFamily: theme.type.mono.fontFamily,
      color: theme.foreground,
    },
    connectHost: { alignSelf: 'stretch' },
    help: {
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.foregroundSubtle,
    },
    error: {
      fontSize: theme.type.body.fontSize,
      lineHeight: theme.type.body.lineHeight,
      color: theme.status.danger,
    },
  });
}
