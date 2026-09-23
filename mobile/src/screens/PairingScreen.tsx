// PairingScreen.tsx is Milestone 1's proof-of-life screen, extracted from
// App.tsx for Milestone 2 (docs/plans/active/mobile-app-milestone-2.md
// Item 2): paste a pairing URL, Connect. The difference from Milestone 1
// is that the connection is NOT one-shot anymore -- on success the live
// RelayConnection is handed to the app's screen stack instead of closing
// it after one workspace.list.
//
// Rebuilt on the token system (mobile-ui-polish plan Item 1): every
// color comes from useAppTheme(), never a hardcoded hex literal.

import { useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { UiHost } from '../ui/UiHost';
import { StatusBar } from 'expo-status-bar';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

type Status = { kind: 'idle' } | { kind: 'connecting' } | { kind: 'error'; message: string };

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

  async function handleConnect() {
    setStatus({ kind: 'connecting' });
    try {
      const conn = await RelayConnection.connect(pairingUrl.trim());
      onConnected(conn);
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  }

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
            label={status.kind === 'connecting' ? 'Connecting…' : 'Connect'}
            onPress={handleConnect}
            disabled={!pairingUrl.trim() || status.kind === 'connecting'}
          />
        </UiHost>
      </View>
      {status.kind === 'error' && <Text style={styles.error}>{status.message}</Text>}
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
    error: {
      color: theme.status.danger,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      marginTop: theme.spacing[4],
    },
  });
}
