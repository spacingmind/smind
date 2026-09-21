// TaskDetailScreen.tsx is Milestone 2's Item 3 screen grown by
// Milestone 3's Item 1: the task's most recent run's transcript,
// read-only, PLUS a compose box for sending a follow-up prompt (only
// when a prior run exists to infer a provider from -- a zero-runs task
// keeps the empty state; see docs/plans/active/
// mobile-app-milestone-3.md's Decisions). History comes from run.logs;
// if the run is still active, run.attach streams live events over the
// same persistent connection (via call()'s request-scoped onEvent).
// Sending is run.start + a second run.attach on the new run (not
// task.prompt, whose cancellation would stop the run); the sent text
// renders immediately and the new run's events append to the same
// timeline. Navigating back detaches both attaches cleanly
// (task.cancel per conn.go's cancellation shape) with no leaked
// handlers. No permission approval or push notifications yet --
// permission approval is Item 2, push is Milestone 4.

import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { listRunsForTask, RunSummary } from '../api';
import { sendFollowUpPrompt, RunTail } from '../followUpPrompt';
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
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const liveSeq = useRef(0);
  const followUpTails = useRef<RunTail[]>([]);

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

        if (latest.Status === 'running') {
          // A live run: run.attach alone. It backfills the run's whole
          // recorded history before the live tail (internal/runs
          // Registry.Subscribe), so a separate run.logs fetch here would
          // double-render every historical line -- the web UI's
          // streamRun makes the same choice.
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
        } else {
          // A finished run: one run.logs fetch, the full transcript.
          const logs = (await conn.call('run.logs', { runId: latest.ID })) as { events?: unknown[] };
          if (cancelled) return;
          setLines(timelineFromLogs(logs as { events?: never[] }));
        }
      } catch (e) {
        if (!cancelled) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      }
    })();

    return () => {
      cancelled = true;
      attach?.cancel(); // clean detach: run keeps going server-side
      // Any follow-up run's attach detaches the same way (Item 1).
      for (const tail of followUpTails.current) tail.cancel();
      followUpTails.current = [];
    };
  }, [conn, taskId]);

  const nextSeq = useCallback(() => {
    liveSeq.current++;
    return 1_000_000 + liveSeq.current;
  }, []);

  const appendLines = useCallback((newLines: TimelineLine[]) => {
    setLines((prev) => [...prev, ...newLines]);
  }, []);

  const removeLines = useCallback((doomed: TimelineLine[]) => {
    const keys = new Set(doomed.map((l) => l.key));
    setLines((prev) => prev.filter((l) => !keys.has(l.key)));
  }, []);

  const trackTail = useCallback((tail: RunTail) => {
    followUpTails.current.push(tail);
  }, []);

  const handleSend = useCallback(async () => {
    const text = draft.trim();
    if (!text || state.kind !== 'ready' || state.run === null) return;
    setDraft('');
    setSendError(null);
    try {
      await sendFollowUpPrompt(conn, { appendLines, removeLines, trackTail }, taskId, state.run.Provider, text, nextSeq);
    } catch (e) {
      setDraft(text); // the typed text is never silently lost
      setSendError(e instanceof Error ? e.message : String(e));
    }
  }, [appendLines, conn, draft, nextSeq, removeLines, state, taskId, trackTail]);

  const canSend = draft.trim().length > 0 && state.kind === 'ready' && state.run !== null;

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
          {sendError !== null && <Text style={styles.sendError}>Couldn't send: {sendError}</Text>}
          <View style={styles.composeRow}>
            <TextInput
              style={styles.composeInput}
              value={draft}
              onChangeText={setDraft}
              placeholder="Send a follow-up prompt…"
              placeholderTextColor="#999"
              multiline
            />
            <TouchableOpacity
              style={[styles.sendButton, !canSend ? styles.sendButtonDisabled : null]}
              onPress={handleSend}
              disabled={!canSend}
            >
              <Text style={styles.sendButtonText}>Send</Text>
            </TouchableOpacity>
          </View>
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
    paddingBottom: 16,
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
  sendError: {
    color: '#dc2626',
    fontSize: 12,
    marginTop: 8,
    marginBottom: 4,
  },
  composeRow: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: '#e5e5e5',
    paddingTop: 8,
    marginTop: 8,
  },
  composeInput: {
    flex: 1,
    borderWidth: 1,
    borderColor: '#d4d4d8',
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingTop: 8,
    paddingBottom: 8,
    fontSize: 14,
    maxHeight: 120,
    marginRight: 8,
  },
  sendButton: {
    backgroundColor: '#2563eb',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 16,
  },
  sendButtonDisabled: {
    backgroundColor: '#93c5fd',
  },
  sendButtonText: {
    color: '#fff',
    fontSize: 14,
    fontWeight: '600',
  },
});
