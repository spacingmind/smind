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
// handlers. No push notifications yet (Milestone 4).
//
// Rebuilt on the token system (mobile-ui-polish plan Item 3): text/
// thinking lines, tool-call rows, permission cards, and the compose box
// all render via useAppTheme(). Tool-call rows are collapsed by default
// (name + title, one line, tap to expand a long title) with an icon for
// the real 3-state status (running/success/failure -- runTimeline.ts's
// toolCall field carries this; there is no "queued" state and no raw
// output-line field on the wire, so no live-output capsule is rendered).

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { Button } from '@expo/ui';
import { listRunsForTask, RunSummary } from '../api';
import { sendFollowUpPrompt, RunTail } from '../followUpPrompt';
import { feedPermissionEvent, PermissionBoard, PermissionRequestState, respondToPermission } from '../permissionRequests';
import { RelayConnection } from '../relay/RelayConnection';
import { lineFromAttachEvent, TimelineLine, timelineFromLogs } from '../runTimeline';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

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

/** {icon, color} for a tool call's real 3-state status -- no "queued" state exists on the wire (internal/taskrunner/event.go). */
function toolStatusIcon(theme: AppTheme, status: string): { icon: string; color: string } {
  switch (status) {
    case 'running':
      return { icon: '●', color: theme.status.running };
    case 'success':
      return { icon: '✓', color: theme.status.success };
    case 'failure':
      return { icon: '✕', color: theme.status.danger };
    default:
      return { icon: '○', color: theme.foregroundMuted };
  }
}

/** The permission card's tint: pending is warning-adjacent, resolved is success, an inline respond error is danger (Item 3's Acceptance Criteria). */
function permissionTint(theme: AppTheme, req: PermissionRequestState): string {
  if (req.error !== null) return theme.status.danger;
  return req.status === 'resolved' ? theme.status.success : theme.status.warning;
}

export function TaskDetailScreen({ conn, taskId, taskTitle, onBack }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [state, setState] = useState<LoadState>({ kind: 'loading' });
  const [lines, setLines] = useState<TimelineLine[]>([]);
  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const [permissions, setPermissions] = useState<PermissionRequestState[]>([]);
  const [expandedToolCalls, setExpandedToolCalls] = useState<ReadonlySet<string>>(new Set());
  const board = useRef(new PermissionBoard()).current;
  const liveSeq = useRef(0);
  const followUpTails = useRef<RunTail[]>([]);

  useEffect(() => {
    let cancelled = false;
    let attach: { cancel(): void } | null = null;
    setState({ kind: 'loading' });
    setLines([]);
    setPermissions([]);
    board.onChange = () => setPermissions(board.list());

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
                if (feedPermissionEvent(board, latest.ID, event, params)) return;
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
          board.applyLogEvents(latest.ID, logs.events ?? []);
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
      await sendFollowUpPrompt(
        conn,
        {
          appendLines,
          removeLines,
          trackTail,
          onEvent: (event, params, runId) => feedPermissionEvent(board, runId, event, params),
        },
        taskId,
        state.run.Provider,
        text,
        nextSeq,
      );
    } catch (e) {
      setDraft(text); // the typed text is never silently lost
      setSendError(e instanceof Error ? e.message : String(e));
    }
  }, [appendLines, conn, draft, nextSeq, removeLines, state, taskId, trackTail]);

  const handlePermissionTap = useCallback(
    (requestId: string, optionId: string) => {
      void respondToPermission(conn, board, requestId, optionId);
    },
    [conn, board],
  );

  const toggleExpanded = useCallback((key: string) => {
    setExpandedToolCalls((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const canSend = draft.trim().length > 0 && state.kind === 'ready' && state.run !== null;

  return (
    <View style={styles.container}>
      <View style={styles.back}>
        <Button variant="text" label="&larr; Back" onPress={onBack} />
      </View>
      <Text style={styles.title}>{taskTitle}</Text>

      {state.kind === 'loading' && <ActivityIndicator style={styles.spinner} size="large" color={theme.primary} />}
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
            {lines.map((line) =>
              line.toolCall ? (
                <ToolCallRow
                  key={line.key}
                  theme={theme}
                  styles={styles}
                  toolCall={line.toolCall}
                  expanded={expandedToolCalls.has(line.key)}
                  onToggle={() => toggleExpanded(line.key)}
                />
              ) : (
                <Text
                  key={line.key}
                  style={line.role === 'user' ? styles.userLine : line.role === '' ? styles.metaLine : styles.assistantLine}
                >
                  {line.role !== '' ? `${line.role}: ` : ''}
                  {line.text}
                </Text>
              ),
            )}
          </ScrollView>
          {permissions.map((req) => {
            const tint = permissionTint(theme, req);
            return (
              <View key={req.requestId} style={[styles.permissionCard, { borderColor: tint }]}>
                <Text style={styles.permissionSummary}>{req.summary}</Text>
                {req.status === 'pending' ? (
                  <View style={styles.permissionOptions}>
                    {req.options.map((opt) => (
                      <View key={opt.id} style={styles.permissionButton}>
                        <Button label={opt.label} onPress={() => handlePermissionTap(req.requestId, opt.id)} />
                      </View>
                    ))}
                  </View>
                ) : (
                  <Text style={[styles.permissionResolved, { color: tint }]}>
                    {req.resolvedWith?.by === 'tap' ? 'chosen: ' : 'resolved: '}
                    {req.options.find((o) => o.id === req.resolvedWith?.optionId)?.label ?? req.resolvedWith?.optionId ?? '?'}
                  </Text>
                )}
                {req.error !== null && <Text style={styles.permissionError}>Couldn't respond: {req.error}</Text>}
              </View>
            );
          })}
          {sendError !== null && <Text style={styles.sendError}>Couldn't send: {sendError}</Text>}
          <View style={styles.composeRow}>
            <TextInput
              style={styles.composeInput}
              value={draft}
              onChangeText={setDraft}
              placeholder="Send a follow-up prompt…"
              placeholderTextColor={theme.foregroundMuted}
              multiline
            />
            <Button label="Send" onPress={handleSend} disabled={!canSend} />
          </View>
        </>
      )}
    </View>
  );
}

/** One collapsed-by-default tool-call row: icon for the real status, name + title, tap to expand a long title. */
function ToolCallRow({
  theme,
  styles,
  toolCall,
  expanded,
  onToggle,
}: {
  theme: AppTheme;
  styles: ReturnType<typeof makeStyles>;
  toolCall: { toolName: string; title: string; status: string };
  expanded: boolean;
  onToggle: () => void;
}) {
  const { icon, color } = toolStatusIcon(theme, toolCall.status);
  return (
    <TouchableOpacity style={styles.toolCallRow} onPress={onToggle} activeOpacity={0.7}>
      <Text style={[styles.toolCallIcon, { color }]}>{icon}</Text>
      <Text style={styles.toolCallName}>{toolCall.toolName}</Text>
      {toolCall.title.length > 0 && (
        <Text style={styles.toolCallTitle} numberOfLines={expanded ? undefined : 1}>
          {toolCall.title}
        </Text>
      )}
    </TouchableOpacity>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.surface[0],
      paddingTop: theme.spacing[16],
      paddingHorizontal: theme.spacing[4],
      paddingBottom: theme.spacing[4],
    },
    back: {
      alignSelf: 'flex-start',
      marginBottom: theme.spacing[2],
    },
    title: {
      fontSize: theme.type.sectionTitle.fontSize,
      lineHeight: theme.type.sectionTitle.lineHeight,
      fontWeight: theme.type.sectionTitle.fontWeight,
      color: theme.foreground,
      marginBottom: theme.spacing[1],
    },
    runMeta: {
      fontSize: theme.type.metadataLabel.fontSize,
      color: theme.foregroundMuted,
      marginBottom: theme.spacing[3],
    },
    spinner: {
      marginTop: theme.spacing[8],
    },
    transcript: {
      flex: 1,
    },
    userLine: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.primary,
      marginBottom: theme.spacing[2],
    },
    assistantLine: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.foreground,
      marginBottom: theme.spacing[2],
    },
    metaLine: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.metadataLabel.fontSize,
      color: theme.foregroundMuted,
      marginBottom: theme.spacing[2],
    },
    toolCallRow: {
      flexDirection: 'row',
      alignItems: 'center',
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.sm,
      paddingVertical: theme.spacing[1.5],
      paddingHorizontal: theme.spacing[2],
      marginBottom: theme.spacing[2],
    },
    toolCallIcon: {
      fontSize: theme.type.metadataLabel.fontSize,
      marginRight: theme.spacing[1.5],
    },
    toolCallName: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.codeAnnotation.fontSize,
      fontWeight: theme.type.codeAnnotation.fontWeight,
      color: theme.foreground,
      marginRight: theme.spacing[1.5],
    },
    toolCallTitle: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.foregroundMuted,
      flex: 1,
    },
    empty: {
      color: theme.foregroundMuted,
      fontSize: theme.type.interface.fontSize,
      marginTop: theme.spacing[4],
    },
    errorText: {
      color: theme.status.danger,
      fontWeight: theme.type.metadataLabel.fontWeight,
      marginBottom: theme.spacing[1],
    },
    errorDetail: {
      fontFamily: theme.type.codeAnnotation.fontFamily,
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.foregroundMuted,
    },
    permissionCard: {
      backgroundColor: theme.surface[1],
      borderWidth: 1,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
      marginTop: theme.spacing[2],
    },
    permissionSummary: {
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.foreground,
      marginBottom: theme.spacing[2],
    },
    permissionOptions: {
      flexDirection: 'row',
      flexWrap: 'wrap',
    },
    permissionButton: {
      marginBottom: theme.spacing[1],
      marginRight: theme.spacing[2],
    },
    permissionResolved: {
      fontSize: theme.type.metadataLabel.fontSize,
    },
    permissionError: {
      fontSize: theme.type.metadataLabel.fontSize,
      color: theme.status.danger,
      marginTop: theme.spacing[1],
    },
    sendError: {
      color: theme.status.danger,
      fontSize: theme.type.metadataLabel.fontSize,
      marginTop: theme.spacing[2],
      marginBottom: theme.spacing[1],
    },
    composeRow: {
      flexDirection: 'row',
      alignItems: 'flex-end',
      borderTopWidth: 1,
      borderTopColor: theme.border,
      paddingTop: theme.spacing[2],
      marginTop: theme.spacing[2],
    },
    composeInput: {
      flex: 1,
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.md,
      paddingHorizontal: theme.spacing[2.5],
      paddingTop: theme.spacing[2],
      paddingBottom: theme.spacing[2],
      fontSize: theme.type.interface.fontSize,
      color: theme.foreground,
      maxHeight: 120,
      marginRight: theme.spacing[2],
    },
  });
}
