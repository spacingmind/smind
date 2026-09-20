// TaskDetailScreen.tsx is Milestone 2's Item 3 screen (placeholder while
// Item 2 lands; the realtime timeline lands with Item 3): shows the task
// title and a back button, fetching its most recent run via run.list.

import { useEffect, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Task } from '../api';
import { RelayConnection } from '../relay/RelayConnection';

interface Props {
  conn: RelayConnection;
  task: Task;
  onBack: () => void;
}

export function TaskDetailScreen({ conn, task, onBack }: Props) {
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [detail, setDetail] = useState<string>('');

  useEffect(() => {
    let cancelled = false;
    setStatus('loading');
    conn
      .call('run.list')
      .then((all) => {
        if (cancelled) return;
        const mine = (all as Array<{ ID: string; TaskID: number; Status: string }>).filter((r) => r.TaskID === task.ID);
        setDetail(mine.length === 0 ? 'No runs yet' : `${mine.length} run(s), most recent: ${mine[0].Status}`);
        setStatus('ready');
      })
      .catch(() => !cancelled && setStatus('error'));
    return () => {
      cancelled = true;
    };
  }, [conn, task.ID]);

  return (
    <View style={styles.container}>
      <TouchableOpacity onPress={onBack} style={styles.back}>
        <Text style={styles.backText}>&larr; Back</Text>
      </TouchableOpacity>
      <Text style={styles.title}>{task.Title}</Text>
      {status === 'loading' && <ActivityIndicator />}
      {status === 'ready' && <Text style={styles.detail}>{detail}</Text>}
      {status === 'error' && <Text style={styles.error}>Failed to load runs.</Text>}
      <ScrollView />
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
  back: {
    alignSelf: 'flex-start',
    marginBottom: 8,
  },
  backText: {
    color: '#2563eb',
    fontSize: 15,
  },
  title: {
    fontSize: 22,
    fontWeight: '700',
    marginBottom: 16,
  },
  detail: {
    fontSize: 15,
    color: '#333',
  },
  error: {
    color: '#dc2626',
  },
});
