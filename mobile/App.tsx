// App.tsx is Milestone 1's entire UI (docs/plans/active/mobile-app-milestone-1.md's
// Item 3 Decisions): one screen, a pairing URL text field, and a Connect
// button that runs the full admission -> E2EE handshake -> workspace.list
// chain and renders the raw JSON response. No task list, no navigation, no
// QR scanning -- that's Milestone 2+.

import { useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { connectAndFetchWorkspaceList } from './src/relay/client';

type Status = { kind: 'idle' } | { kind: 'connecting' } | { kind: 'success'; json: string } | { kind: 'error'; message: string };

export default function App() {
  const [pairingUrl, setPairingUrl] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  async function handleConnect() {
    setStatus({ kind: 'connecting' });
    try {
      const result = await connectAndFetchWorkspaceList(pairingUrl.trim());
      setStatus({ kind: 'success', json: result.workspaceListJSON });
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  }

  return (
    <View style={styles.container}>
      <StatusBar style="auto" />
      <Text style={styles.title}>smind pairing (milestone 1)</Text>
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

      <ScrollView style={styles.resultBox}>
        {status.kind === 'idle' && <Text style={styles.hint}>Nothing yet.</Text>}
        {status.kind === 'error' && <Text style={styles.error}>{status.message}</Text>}
        {status.kind === 'success' && <Text style={styles.json}>{status.json}</Text>}
      </ScrollView>
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
  resultBox: {
    marginTop: 24,
    flex: 1,
  },
  hint: {
    color: '#999',
  },
  error: {
    color: '#dc2626',
    fontFamily: 'monospace',
  },
  json: {
    fontFamily: 'monospace',
    fontSize: 12,
  },
});
