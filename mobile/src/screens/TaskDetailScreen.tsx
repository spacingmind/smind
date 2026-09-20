// TaskDetailScreen.tsx is Milestone 2's Item 3 screen: the task's most
// recent run's transcript, read-only. History comes from run.logs; if the
// run is still active, run.attach streams live events over the same
// persistent connection (via call()'s request-scoped onEvent). Navigating
// back detaches cleanly (task.cancel per conn.go's cancellation shape)
// with no leaked handlers. No prompt box, no permission approval, no push
// notifications -- Milestone 3.

import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { listRunsForTask, RunSummary } from '../api';
import { RelayConnection } from '../relay/RelayConnection';
import { lineFromAttachEvent, TimelineLine, timelineFromLogs } from '../runTimeline';

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; run: RunSummary | null }
  | { kind: 'error'; message: string };

interface Props {
  conn: RelayConnection;
  taskId: number;
  taskTitle: string;
  onBack: () => void;
}

export function TaskDetailScreen({ conn, taskId, taskTitle, onBack }: Props) {
  const [state, setState] = useState<LoadState>({ kind: 'loading' });
  const [lines, setLines] = useState<TimelineLine[]>([]);
  const liveSeq = useRef(0);

  useEffect(() => {
    let cancelled = false;
    let attach: { cancel(): void } | null = null;
    setState({ kind: 'loading' });
    setLines([]);

    (async () => {
      try {
        const runs = await listRunsForTask(conn, taskId);
        if (cancelled) return;
        const latest = runs[0] ?? null;
        setState({ kind: 'ready', run: latest });
        if (!latest) return;

        // History first: the full recorded transcript.
        const logs = (await conn.call('run.logs', { runId: latest.ID })) as { events?: unknown[] };
        if (cancelled) return;
        setLines(timelineFromLogs(logs as { events?: never[] }));

        // Then, only if still active, live tail via run.attach.
        if (latest.Status === 'running') {
          const p = conn.call(
            'run.attach',
            { runId: latest.ID },
            {
              onEvent: (event, params) => {
                if (cancelled) return;
                liveSeq.current++;
                const newLines = lineFromAttachEvent(event, params, 1_000_000 + liveSeq.current);
                if (newLines.length > 0) setLines((prev) => [...prev, ...newLines]);
              },
            },
          );
          attach = p;
          p.catch(() => {
            // Detach (navigate-away cancel) or the connection ending:
            // either way the screen state already says what it needs to.
          });
        }
      } catch (e) {
        if (!cancelled) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      }
    })();

    return () => {
      cancelled = true;
      attach?.cancel(); // clean detach: run keeps going server-side
    };
  }, [conn, taskId]);

  return (
    <View style={styles.container}>
      <TouchableOpacity onPress={onBack} style={styles.back}>
        <Text style={styles.backText}>&larr; Back</Text>
      </TouchableOpacity>
      <Text style={styles.title}>{taskTitle}</Text>

      {state.kind === 'loading' && <ActivityIndicator style={styles.spinner} size="large" />}
      {state.kind === 'error' && (
        <View>
          <Text style={styles.errorText}>Couldn't load this task's runs.</Text>
          <Text style={styles.errorDetail}>{state.message}</Text>
        </View>
      )}
      {state.kind === 'ready' && state.run === null && (
        <Text style={styles.empty}>No runs yet for this task. Start one from the daemon or web UI.</Text>
      )}
      {state.kind === 'ready' && state.run !== null && (
        <>
          <Text style={styles.runMeta}>
            run {state.run.ID.slice(0, 8)} · {state.run.Provider} · {state.run.Status === 'running' ? 'live' : state.run.Status}
          </Text>
          <ScrollView style={styles.transcript}>
            {lines.length === 0 && <Text style={styles.empty}>(empty transcript)</Text>}
            {lines.map((line) => (
              <Text key={line.key} style={line.role === 'user' ? styles.userLine : line.role === '' ? styles.metaLine : styles.assistantLine}>
                {line.role !== '' ? `${line.role}: ` : ''}
                {line.text}
              </Text>
            ))}
          </ScrollView>
        </>
      )}
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
    marginBottom: 4,
  },
  runMeta: {
    fontSize: 12,
    color: '#666',
    marginBottom: 12,
  },
  spinner: {
    marginTop: 32,
  },
  transcript: {
    flex: 1,
  },
  userLine: {
    fontFamily: 'monospace',
    fontSize: 13,
    color: '#1e3a8a',
    marginBottom: 8,
  },
  assistantLine: {
    fontFamily: 'monospace',
    fontSize: 13,
    color: '#111',
    marginBottom: 8,
  },
  metaLine: {
    fontFamily: 'monospace',
    fontSize: 12,
    color: '#666',
    marginBottom: 8,
  },
  empty: {
    color: '#666',
    marginTop: 16,
  },
  errorText: {
    color: '#dc2626',
    fontWeight: '600',
    marginBottom: 4,
  },
  errorDetail: {
    fontFamily: 'monospace',
    fontSize: 12,
    color: '#666',
  },
});
