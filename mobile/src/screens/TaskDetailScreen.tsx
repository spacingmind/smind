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
//
// Queue-aware (ADR-0021 §9): a plain send queues when the chat is busy
// (whenBusy: 'queue') instead of failing; queued prompts render above
// the compose row straight from server state with per-item Cancel; a
// "Send now" button interrupts the live run (whenBusy: 'interrupt');
// runs the daemon starts itself when delivering a queued prompt are
// adopted by RunFollower over run.status and attached exactly once
// (attachedRuns guard shared with sendFollowUpPrompt's shouldAttach).

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { UiHost } from '../ui/UiHost';
import { defaultChatId, listRunsForTask, RunSummary } from '../api';
import { continueRun, isEchoOfOptimisticLine, sendFollowUpPrompt, RunTail } from '../followUpPrompt';
import { feedPermissionEvent, PermissionBoard, PermissionRequestState, respondToPermission } from '../permissionRequests';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { RunFollower, RunStatusEvent } from '../runFollower';
import { describeRunFooter } from '../runFooter';
import { lineFromAttachEvent, TimelineLine, timelineFromLogs } from '../runTimeline';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { useChatQueue } from '../useChatQueue';
import { QueuedPrompts } from './QueuedPrompts';

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
  // ObservableState binding for the @expo/ui compose input (plan Item 2);
  // React state remains the source of truth for canSend and the
  // restore-on-failed-send path.
  const draftState = useNativeState('');
  const [sendError, setSendError] = useState<string | null>(null);
  const [continued, setContinued] = useState(false);
  const [permissions, setPermissions] = useState<PermissionRequestState[]>([]);
  const [expandedToolCalls, setExpandedToolCalls] = useState<ReadonlySet<string>>(new Set());
  const [chatId, setChatId] = useState<number | null>(null);
  const [liveRunId, setLiveRunId] = useState<string | null>(null);
  const { epoch } = useConnectionState(conn); // reload what a drop interrupted (the run attach) once reconnected
  const board = useRef(new PermissionBoard()).current;
  const liveSeq = useRef(0);
  const followUpTails = useRef<RunTail[]>([]);
  const followerRef = useRef<RunFollower | null>(null);
  const attachedRuns = useRef(new Set<string>());
  const lastRunId = useRef<string | null>(null);
  const mounted = useRef(true);
  const chatIdRef = useRef<number | null>(null);
  const refreshSeq = useRef(0);
  const sending = useRef(false);
  const queue = useChatQueue(conn, chatId);

  useEffect(() => {
    mounted.current = true; // set again on a remount (Fast Refresh) -- the cleanup alone would leave it false for good
    return () => {
      mounted.current = false;
    };
  }, []);

  /** Attach to a run and stream its events into the timeline; every run is attached at most once (attachedRuns guard). */
  const attachRun = (runId: string, isCancelled: () => boolean) => {
    if (attachedRuns.current.has(runId)) return;
    attachedRuns.current.add(runId);
    const p = conn.call('run.attach', { runId }, {
      onEvent: (event, params) => {
        if (isCancelled()) return;
        if (feedPermissionEvent(board, runId, event, params)) return;
        liveSeq.current++;
        const newLines = lineFromAttachEvent(event, params, 1_000_000 + liveSeq.current);
        if (newLines.length === 0) return;
        // An echo of a prompt this client already shows optimistically is skipped, so a prompt shows once whichever
        // path (the run.start response or this adoption) attached the run first.
        if (event === 'user_message') setLines((prev) => (isEchoOfOptimisticLine(prev, event, params) ? prev : [...prev, ...newLines]));
        else setLines((prev) => [...prev, ...newLines]);
      },
    });
    followUpTails.current.push(p);
    p.catch(() => {
      // Detach (navigate-away cancel) or the connection ending:
      // either way the screen state already says what it needs to.
    });
  };

  /** Keeps the header meta and the not-finished footer in step with the latest run (e.g. a queued prompt the daemon delivered). */
  const refreshLatest = useCallback(async () => {
    const seq = ++refreshSeq.current; // overlapping refreshes: only the newest answer is applied
    try {
      // "Latest" is per chat (a task can have more than one).
      const runs = await listRunsForTask(conn, taskId, chatIdRef.current ?? undefined);
      if (!mounted.current || seq !== refreshSeq.current) return;
      const next = runs[0] ?? null;
      setState((prev) => (prev.kind === 'ready' ? { kind: 'ready', run: next } : prev));
      if ((next?.ID ?? null) !== lastRunId.current) {
        lastRunId.current = next?.ID ?? null;
        setContinued(false);
      }
      // Re-derive the live flag from the daemon's own list: a run.status the daemon dropped (event.dropped) must not leave
      // "live" stale, nor a live run unattached.
      if (next) {
        followerRef.current?.markKnown(next.ID, next.Status);
        if (next.Status === 'running') attachRun(next.ID, () => !mounted.current);
      }
    } catch {
      // the screen keeps what it has
    }
  }, [conn, taskId]);

  useEffect(() => {
    let cancelled = false;
    let offStatus: (() => void) | null = null;
    let offDropped: (() => void) | null = null;
    setState({ kind: 'loading' });
    setContinued(false);
    setLines([]);
    setPermissions([]);
    attachedRuns.current.clear();
    followerRef.current = null;
    setLiveRunId(null);
    setChatId(null);
    chatIdRef.current = null;
    board.onChange = () => setPermissions(board.list());

    // Subscribe to run.status BEFORE the first await: a run the daemon delivers (or starts) while this screen is still
    // loading must not be missed. Events wait in `early` until the follower (which needs the chat id) exists.
    const early: RunStatusEvent[] = [];
    let follower: RunFollower | null = null;
    offStatus = conn.subscribe(['run.status'], (e) => {
      const ev = e.payload as RunStatusEvent;
      if (follower) follower.onRunStatus(ev);
      else early.push(ev);
    });
    // The daemon dropped pushed events for us: re-read the latest run (the queue refetches itself).
    offDropped = conn.onDropped(() => void refreshLatest());

    (async () => {
      try {
        const runs = await listRunsForTask(conn, taskId);
        if (cancelled) return;
        const latest = runs[0] ?? null;
        setState({ kind: 'ready', run: latest });
        lastRunId.current = latest?.ID ?? null;

        let resolved: number | null = latest?.ChatID ?? null;
        if (resolved === null) {
          try {
            resolved = await defaultChatId(conn, taskId);
          } catch {
            resolved = null;
          }
        }
        if (cancelled) return;
        setChatId(resolved);
        chatIdRef.current = resolved;
        if (resolved !== null) {
          follower = new RunFollower(
            resolved,
            (runId) => {
              attachRun(runId, () => cancelled);
              void refreshLatest();
            },
            (live) => {
              setLiveRunId(live);
              if (live === null) void refreshLatest();
            },
          );
          followerRef.current = follower;
          if (latest) follower.markKnown(latest.ID, latest.Status);
          for (const ev of early) follower.onRunStatus(ev); // what arrived while loading
          early.length = 0;
        }

        if (!latest) return;

        if (latest.Status === 'running') {
          // A live run: run.attach alone. It backfills the run's whole
          // recorded history before the live tail (internal/runs
          // Registry.Subscribe), so a separate run.logs fetch here would
          // double-render every historical line -- the web UI's
          // streamRun makes the same choice.
          attachRun(latest.ID, () => cancelled);
        } else {
          // A finished run: one run.logs fetch, the full transcript.
          const logs = (await conn.call('run.logs', { runId: latest.ID })) as { events?: unknown[] };
          if (cancelled) return;
          board.applyLogEvents(latest.ID, logs.events ?? []);
          // Merge, do not overwrite: a prompt sent or a run adopted while the logs were loading has already appended lines.
          const logLines = timelineFromLogs(logs as { events?: never[] });
          setLines((prev) => [...logLines, ...prev]);
        }
      } catch (e) {
        if (!cancelled) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      }
    })();

    return () => {
      cancelled = true;
      offStatus?.();
      offDropped?.();
      followerRef.current = null;
      // Any follow-up run's attach detaches the same way (Item 1).
      for (const tail of followUpTails.current) tail.cancel();
      followUpTails.current = [];
    };
  }, [conn, taskId, epoch, refreshLatest]);

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

  const send = useCallback(
    async (whenBusy: 'queue' | 'interrupt') => {
      const text = draft.trim();
      if (!text || state.kind !== 'ready' || state.run === null) return;
      if (sending.current) return; // one send at a time: a second press before the re-render must not queue the prompt twice
      sending.current = true;
      setDraft('');
      draftState.value = '';
      setSendError(null);
      try {
        const result = await sendFollowUpPrompt(
          conn,
          {
            appendLines,
            removeLines,
            trackTail,
            onEvent: (event, params, runId) => feedPermissionEvent(board, runId, event, params),
            shouldAttach: (runId) => {
              if (attachedRuns.current.has(runId)) return false;
              attachedRuns.current.add(runId);
              return true;
            },
          },
          taskId,
          state.run.Provider,
          text,
          nextSeq,
          { whenBusy, chatId: chatId ?? undefined },
        );
        if (result.kind === 'queued') queue.refresh(); // covers a missed snapshot
      } catch (e) {
        setDraft(text); // the typed text is never silently lost
        draftState.value = text;
        setSendError(e instanceof Error ? e.message : String(e));
      } finally {
        sending.current = false;
      }
    },
    [appendLines, board, chatId, conn, draft, nextSeq, queue, removeLines, state, taskId, trackTail],
  );

  const handleSend = useCallback(() => send('queue'), [send]);
  const handleSendNow = useCallback(() => send('interrupt'), [send]);

  const handleContinue = useCallback(async () => {
    if (continued || state.kind !== 'ready' || state.run === null) return;
    setContinued(true);
    setSendError(null);
    try {
      const result = await continueRun(
        conn,
        {
          appendLines,
          removeLines,
          trackTail,
          onEvent: (event, params, runId) => feedPermissionEvent(board, runId, event, params),
          shouldAttach: (runId) => {
            if (attachedRuns.current.has(runId)) return false;
            attachedRuns.current.add(runId);
            return true;
          },
        },
        taskId,
        state.run,
        nextSeq,
        chatId ?? undefined,
      );
      if (result.kind === 'queued') queue.refresh();
    } catch (e) {
      setContinued(false);
      setSendError(e instanceof Error ? e.message : String(e));
    }
  }, [appendLines, board, chatId, conn, continued, nextSeq, queue, removeLines, state, taskId, trackTail]);

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
        <UiHost>
          <Button variant="text" label="&larr; Back" onPress={onBack} />
        </UiHost>
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
          {(() => {
            const footer = describeRunFooter(state.run);
            return footer !== null && !continued && liveRunId === null ? (
              <View style={styles.footerRow}>
                <Text style={[styles.footerText, { color: theme.status.warning }]}>{footer.sentence}</Text>
                <UiHost>
                  <Button label="Continue" onPress={handleContinue} />
                </UiHost>
              </View>
            ) : null;
          })()}
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
                      <UiHost key={opt.id} style={styles.permissionButton}>
                        <Button label={opt.label} onPress={() => handlePermissionTap(req.requestId, opt.id)} />
                      </UiHost>
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
          <QueuedPrompts items={queue.items} onCancel={(id) => void queue.cancel(id)} />
          <View style={styles.composeRow}>
            <UiHost style={styles.composeWrap}>
              <ExpoTextInput
                style={styles.composeField}
                textStyle={styles.composeText}
                value={draftState}
                onChangeText={(text) => {
                  draftState.value = text;
                  setDraft(text);
                }}
                placeholder={liveRunId !== null ? 'Queue a follow-up — it sends when this run finishes' : 'Send a follow-up prompt…'}
                placeholderTextColor={theme.foregroundMuted}
                multiline
              />
            </UiHost>
            {liveRunId !== null && canSend && (
              <UiHost style={styles.sendHost}>
                <Button label="Send now" onPress={handleSendNow} />
              </UiHost>
            )}
            <UiHost style={styles.sendHost}>
              <Button label="Send" onPress={handleSend} disabled={!canSend} />
            </UiHost>
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
    footerRow: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      gap: theme.spacing[2],
      marginBottom: theme.spacing[2],
    },
    footerText: {
      flex: 1,
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      fontWeight: theme.type.interface.fontWeight,
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
    composeWrap: {
      flex: 1,
      maxHeight: 120,
      marginRight: theme.spacing[2],
    },
    sendHost: {
      alignSelf: 'flex-end',
    },
    composeField: {
      width: '100%',
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.md,
      paddingHorizontal: theme.spacing[2.5],
      paddingVertical: theme.spacing[2],
    },
    composeText: {
      fontSize: theme.type.interface.fontSize,
      color: theme.foreground,
    },
  });
}
