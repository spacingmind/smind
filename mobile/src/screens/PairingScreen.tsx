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
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
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
      <TextInput
        style={styles.input}
        multiline
        autoCapitalize="none"
        autoCorrect={false}
        placeholder="https://spacingmind.sh/pair#offer=..."
        placeholderTextColor={theme.foregroundMuted}
        value={pairingUrl}
        onChangeText={setPairingUrl}
      />
      <TouchableOpacity
        style={[styles.button, (!pairingUrl.trim() || status.kind === 'connecting') && styles.buttonDisabled]}
        onPress={handleConnect}
        disabled={!pairingUrl.trim() || status.kind === 'connecting'}
      >
        {status.kind === 'connecting' ? (
          <ActivityIndicator color={theme.surface[0]} />
        ) : (
          <Text style={styles.buttonText}>Connect</Text>
        )}
      </TouchableOpacity>
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
    input: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
      minHeight: 80,
      fontSize: theme.type.codeAnnotation.fontSize,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      color: theme.foreground,
      textAlignVertical: 'top',
    },
    button: {
      backgroundColor: theme.primary,
      borderRadius: theme.radius.md,
      paddingVertical: theme.spacing[3.5],
      alignItems: 'center',
      marginTop: theme.spacing[4],
    },
    buttonDisabled: {
      opacity: 0.5,
    },
    buttonText: {
      color: theme.surface[0],
      fontSize: theme.type.interface.fontSize,
      fontWeight: theme.type.interface.fontWeight,
    },
    error: {
      color: theme.status.danger,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      marginTop: theme.spacing[4],
    },
  });
}
