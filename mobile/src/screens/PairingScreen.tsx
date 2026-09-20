// PairingScreen.tsx is Milestone 1's proof-of-life screen, extracted from
// App.tsx for Milestone 2 (docs/plans/active/mobile-app-milestone-2.md
// Item 2): paste a pairing URL, Connect. The difference from Milestone 1
// is that the connection is NOT one-shot anymore -- on success the live
// RelayConnection is handed to the app's screen stack instead of closing
// it after one workspace.list.

import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { RelayConnection } from '../relay/RelayConnection';

type Status = { kind: 'idle' } | { kind: 'connecting' } | { kind: 'error'; message: string };

interface Props {
  onConnected: (conn: RelayConnection) => void;
}

export function PairingScreen({ onConnected }: Props) {
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
        value={pairingUrl}
        onChangeText={setPairingUrl}
      />
      <TouchableOpacity
        style={[styles.button, (!pairingUrl.trim() || status.kind === 'connecting') && styles.buttonDisabled]}
        onPress={handleConnect}
        disabled={!pairingUrl.trim() || status.kind === 'connecting'}
      >
        {status.kind === 'connecting' ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Connect</Text>}
      </TouchableOpacity>
      {status.kind === 'error' && <Text style={styles.error}>{status.message}</Text>}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#fff',
    paddingTop: 64,
    paddingHorizontal: 16,
  },
  title: {
    fontSize: 20,
    fontWeight: '600',
    marginBottom: 16,
  },
  label: {
    fontSize: 14,
    color: '#444',
    marginBottom: 8,
  },
  input: {
    borderWidth: 1,
    borderColor: '#ccc',
    borderRadius: 8,
    padding: 12,
    minHeight: 80,
    fontSize: 13,
    fontFamily: 'monospace',
    textAlignVertical: 'top',
  },
  button: {
    backgroundColor: '#2563eb',
    borderRadius: 8,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 16,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#fff',
    fontSize: 16,
    fontWeight: '600',
  },
  error: {
    color: '#dc2626',
    fontFamily: 'monospace',
    marginTop: 16,
  },
});
