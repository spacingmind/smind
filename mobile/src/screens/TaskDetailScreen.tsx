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
//
// A live run can be stopped (run.stop, web's composer handleStop) with
// a Stop/Stopping… control driven by stopControlView; a daemon-delivered
// (adopted) run's prompt is not in its streamed events, so the adoption
// path buffers run.attach events until adoptedPromptText resolves and
// prepends the prompt as a user line (keyed p<runId>).

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { FlatList, ListRenderItemInfo, NativeScrollEvent, NativeSyntheticEvent, StyleSheet, View } from 'react-native';
import { Button, useNativeState } from '@expo/ui';
import { useRouter } from 'expo-router';
import { KeyboardAvoidingView } from 'react-native-keyboard-controller';
import { UiHost } from '../ui/UiHost';
import { useScreenInsets } from '../ui/useScreenInsets';
import { adoptedPromptText } from '../adoptedPrompt';
import { addPick, capabilities, PendingAttachment, Pick } from '../attachments';
import { attachMenuItems } from '../attachmentTray';
import { createImageDeps, pickFiles, pickPhotos } from '../attachmentDeps';
import { sendWithAttachments } from '../attachSend';
import { lineAttachmentsFromRun } from '../lineAttachments';
import { createThumbLoader } from '../thumbLoader';
import { AttachmentsInfo, defaultChatId, fetchDaemonInfo, fetchRunAttachment, listProviders, listRunsForTask, ProviderInfo, RunSummary } from '../api';
import { firstSendProvider } from '../firstSend';
import { withRunPrompt } from '../promptLine';
import { taskHref } from '../navigation/routes';
import { recallSelection } from '../newTaskPrefs';
import { continueRun, isEchoOfOptimisticLine, sendFollowUpPrompt, RunTail } from '../followUpPrompt';
import { feedPermissionEvent, PermissionBoard, PermissionRequestState, respondToPermission } from '../permissionRequests';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { RunFollower, RunStatusEvent } from '../runFollower';
import { hapticFor } from '../haptics';
import { findHeldUntil, findSuccessor } from '../failoverContext';
import { describeContinues, describeRunFooter } from '../runFooter';
import { lineFromAttachEvent, TimelineLine, timelineFromLogs } from '../runTimeline';
import { requestStop, stopControlView } from '../stopControl';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { useChatQueue } from '../useChatQueue';
import { ReconnectingBanner } from '../ui/Banner';
import { buildTranscript, TranscriptItem } from '../transcript/coalesceMessages';
import { nextFollowState } from '../transcript/stickToBottom';
import { EmptyState } from '../ui/EmptyState';
import { AssistantMessage } from './task/AssistantMessage';
import { Composer } from './task/Composer';
import { FooterBanner } from './task/FooterBanner';
import { LoadingState } from './task/LoadingState';
import { MetaLine } from './task/MetaLine';
import { PermissionCard } from './task/PermissionCard';
import { RunStrip } from './task/RunStrip';
import { TaskTreeSection } from './task/TaskTreeSection';
import { ThinkingRow } from './task/ThinkingRow';
import { ToolCallRow } from './task/ToolCallRow';
import { UserBubble } from './task/UserBubble';

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; run: RunSummary | null }
  | { kind: 'error'; message: string };

interface Props {
  conn: RelayConnection;
  taskId: number;
}

export function TaskDetailScreen({ conn, taskId }: Props) {
  const theme = useAppTheme();
  const router = useRouter();
  const openTask = useCallback((id: number, title: string) => router.push(taskHref({ ID: id, Title: title })), [router]);
  const insets = useScreenInsets();
  const styles = useMemo(() => makeStyles(theme, insets.bottom), [theme, insets.bottom]);
  const [state, setState] = useState<LoadState>({ kind: 'loading' });
  const [lines, setLines] = useState<TimelineLine[]>([]);
  const [draft, setDraft] = useState('');
  // ObservableState binding for the @expo/ui compose input (plan Item 2);
  // React state remains the source of truth for canSend and the
  // restore-on-failed-send path.
  const draftState = useNativeState('');
  const [sendError, setSendError] = useState<string | null>(null);
  // ADR-0038 prompt attachments: the tray, the inline refusal text, and the daemon's accepted kinds (undefined until loaded / older daemon).
  const [pending, setPending] = useState<PendingAttachment[]>([]);
  const pendingRef = useRef<PendingAttachment[]>([]);
  pendingRef.current = pending;
  const [attachNotice, setAttachNotice] = useState<string | null>(null);
  const [attachInfo, setAttachInfo] = useState<AttachmentsInfo | undefined>(undefined);
  const picking = useRef(false);
  const imageDims = useRef(new Map<string, { width: number; height: number }>()).current;
  // One loader per connection: timeline thumbnails fetch one at a time and are cached for the screen's life.
  const thumbLoader = useMemo(() => createThumbLoader((runId, id) => fetchRunAttachment(conn, runId, id)), [conn]);
  const loadThumb = useCallback((runId: string, id: string) => thumbLoader.load(runId, id), [thumbLoader]);
  const imageDeps = useMemo(() => createImageDeps(imageDims), [imageDims]);
  // The provider catalog for a task with no run (its first prompt has no run to inherit a provider from); null = not loaded yet.
  const [providers, setProviders] = useState<ProviderInfo[] | null>(null);
  const [continued, setContinued] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [permissions, setPermissions] = useState<PermissionRequestState[]>([]);
  const [expandedToolCalls, setExpandedToolCalls] = useState<ReadonlySet<string>>(new Set());
  const [chatId, setChatId] = useState<number | null>(null);
  const [liveRunId, setLiveRunId] = useState<string | null>(null);
  const { epoch, state: connState } = useConnectionState(conn); // reload what a drop interrupted (the run attach) once reconnected
  const board = useRef(new PermissionBoard()).current;
  const liveSeq = useRef(0);
  const followUpTails = useRef<RunTail[]>([]);
  const followerRef = useRef<RunFollower | null>(null);
  const attachedRuns = useRef(new Set<string>());
  const promptShown = useRef(new Set<string>());
  const lastRunId = useRef<string | null>(null);
  const mounted = useRef(true);
  const chatIdRef = useRef<number | null>(null);
  const refreshSeq = useRef(0);
  const sending = useRef(false);
  const queue = useChatQueue(conn, chatId);
  // ADR-0041: the failover successor of a rate-limited latest run, found with one run.list (no polling); null = none/unknown.
  const [successor, setSuccessor] = useState<{ provider: string; chatId: number } | null>(null);

  useEffect(() => {
    let alive = true;
    fetchDaemonInfo(conn)
      .then((info) => alive && setAttachInfo(info.attachments))
      .catch(() => alive && setAttachInfo(undefined)); // unknown: the menu falls back to image + file, the send re-check guards the rest
    return () => {
      alive = false;
    };
  }, [conn]);
  const queueRef = useRef(queue); // the adoption callback lives in the load effect: it reads the current queue through this
  queueRef.current = queue;

  useEffect(() => {
    mounted.current = true; // set again on a remount (Fast Refresh) -- the cleanup alone would leave it false for good
    return () => {
      mounted.current = false;
    };
  }, []);

  /**
   * Attach to a run and stream its events into the timeline; every run is attached at most once (attachedRuns guard).
   * The run's own prompt comes first and its events are buffered until that line is in: a run.start / queue-delivered run's
   * history has no user message, and the attach sites (the load, a run.status adoption, a refresh that finds a run already
   * live) race each other, so the prompt cannot depend on which one wins.
   */
  const attachRun = (runId: string, isCancelled: () => boolean, attachments?: TimelineLine['attachments']) => {
    const prelude = () =>
      adoptedPromptText({ promptForRun: (id) => queueRef.current.promptForRun(id) }, () => listRunsForTask(conn, taskId, chatIdRef.current ?? undefined), runId);
    if (attachedRuns.current.has(runId)) return;
    attachedRuns.current.add(runId);
    const handle = (event: string, params: unknown) => {
      if (isCancelled()) return;
      if (feedPermissionEvent(board, runId, event, params)) return;
      liveSeq.current++;
      const newLines = lineFromAttachEvent(event, params, 1_000_000 + liveSeq.current);
      if (newLines.length === 0) return;
      // An echo of a prompt this client already shows optimistically is skipped, so a prompt shows once whichever
      // path (the run.start response or this adoption) attached the run first.
      if (event === 'user_message') setLines((prev) => (isEchoOfOptimisticLine(prev, event, params) ? prev : [...prev, ...newLines]));
      else setLines((prev) => [...prev, ...newLines]);
    };
    let ready = false;
    const buffered: Array<[string, unknown]> = [];
    const p = conn.call('run.attach', { runId }, {
      onEvent: (event: string, params: unknown) => {
        if (ready) handle(event, params);
        else buffered.push([event, params]);
      },
    });
    followUpTails.current.push(p);
    p.catch(() => {
      // Detach (navigate-away cancel) or the connection ending:
      // either way the screen state already says what it needs to.
    });
    void prelude()
      .then((text) => {
        if (text && !isCancelled() && !promptShown.current.has(runId)) {
          promptShown.current.add(runId);
          const line = { key: `p${runId}`, role: 'user', text } as TimelineLine;
          if (attachments) line.attachments = attachments;
          setLines((prev) => (prev.some((l) => l.role === 'user' && l.key.startsWith('f') && l.text === text) ? prev : [...prev, line]));
        }
      })
      .catch(() => {})
      .finally(() => {
        ready = true;
        for (const [e, params] of buffered.splice(0)) handle(e, params);
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
        if (next.Status === 'running') attachRun(next.ID, () => !mounted.current, lineAttachmentsFromRun(next));
      }
    } catch {
      // the screen keeps what it has
    }
  }, [conn, taskId]);

  // A zero-run task loads the provider catalog once so its composer can resolve a first-send provider;
  // a failure leaves the composer disabled and surfaces through sendError like a failed send.
  const zeroRun = state.kind === 'ready' && state.run === null;
  useEffect(() => {
    if (!zeroRun) return;
    let cancelled = false;
    listProviders(conn)
      .then((catalog) => {
        if (!cancelled) setProviders(catalog);
      })
      .catch((e) => {
        if (!cancelled) setSendError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [conn, zeroRun]);

  useEffect(() => {
    let cancelled = false;
    let offStatus: (() => void) | null = null;
    let offDropped: (() => void) | null = null;
    setState({ kind: 'loading' });
    setProviders(null);
    setContinued(false);
    setLines([]);
    setPermissions([]);
    attachedRuns.current.clear();
    promptShown.current.clear();
    followerRef.current = null;
    setLiveRunId(null);
    setChatId(null);
    chatIdRef.current = null;
    // PermissionBoard mutates its request objects in place: hand the memoised PermissionCard fresh copies so a state change re-renders it.
    board.onChange = () => setPermissions(board.list().map((r) => ({ ...r })));

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
          attachRun(latest.ID, () => cancelled, lineAttachmentsFromRun(latest));
        } else {
          // A finished run: one run.logs fetch, the full transcript.
          const logs = (await conn.call('run.logs', { runId: latest.ID })) as { events?: unknown[] };
          if (cancelled) return;
          board.applyLogEvents(latest.ID, logs.events ?? []);
          // Merge, do not overwrite: a prompt sent or a run adopted while the logs were loading has already appended lines.
          const logLines = timelineFromLogs(logs as { events?: never[] });
          // A run started by run.start carries no user message in its logs: show the run row's own prompt when the logs
          // have no user line (and a prompt sent while the logs loaded has not already rendered it).
          setLines((prev) => {
            // The optimistic line of this very prompt may already have rendered while the logs loaded.
            if (prev.some((l) => l.role === 'user' && l.text === latest.Prompt)) return [...logLines, ...prev];
            return [...withRunPrompt(logLines, latest.Prompt, lineAttachmentsFromRun(latest)), ...prev];
          });
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

  // The provider a send uses — the run's own when it has one, else the first-send resolution over the catalog.
  const sendProvider = state.kind === 'ready' ? firstSendProvider(providers ?? [], state.run?.Provider ?? null, recallSelection()) : null;

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
      if (!text || state.kind !== 'ready' || sendProvider === null) return;
      if (sending.current) return; // one send at a time: a second press before the re-render must not queue the prompt twice
      sending.current = true;
      hapticFor('send'); // same frame as the draft clearing, before the await
      setDraft('');
      draftState.value = '';
      setSendError(null);
      setAttachNotice(null);
      const tray = pendingRef.current;
      const outcome = await sendWithAttachments({ taskId, provider: sendProvider, prompt: text, chatId: chatId ?? undefined, whenBusy }, tray, () =>
        sendFollowUpPrompt(
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
          sendProvider,
          text,
          nextSeq,
          { whenBusy, chatId: chatId ?? undefined, attachments: tray },
        ),
      );
      try {
        if (outcome.kind === 'sent') {
          setPending([]); // the tray goes only once the daemon accepted the prompt
          if (outcome.result.kind === 'queued') queue.refresh(); // covers a missed snapshot
          else if (state.run === null) void refreshLatest(); // a first send's run must adopt (RunStrip/footer) like a task that had one
        } else {
          hapticFor('error');
          setDraft(text); // the typed text and the tray are never silently lost
          draftState.value = text;
          if (outcome.kind === 'refused') setAttachNotice(outcome.notice);
          else if (outcome.attachment) setAttachNotice(outcome.message);
          else setSendError(outcome.message);
        }
      } finally {
        sending.current = false;
      }
    },
    [appendLines, board, chatId, conn, draft, nextSeq, queue, refreshLatest, removeLines, sendProvider, taskId, trackTail],
  );

  const caps = useMemo(() => capabilities(attachInfo), [attachInfo]);
  const attachMenu = useMemo(() => attachMenuItems(attachInfo), [attachInfo]);

  const handleAttach = useCallback(
    async (key: 'photo' | 'file') => {
      if (picking.current || sendProvider === null) return;
      picking.current = true;
      setAttachNotice(null);
      try {
        const remaining = caps.maxCount - pendingRef.current.length;
        const picks: Pick[] = key === 'photo' ? await pickPhotos(remaining) : await pickFiles();
        for (const p of picks) if (p.width !== undefined && p.height !== undefined) imageDims.set(p.uri, { width: p.width, height: p.height });
        const base = { taskId, provider: sendProvider, prompt: draft.trim(), chatId: chatId ?? undefined, whenBusy: 'queue' };
        let existing = pendingRef.current;
        const refusals: string[] = [];
        const added: PendingAttachment[] = [];
        for (const p of picks) {
          try {
            const res = await addPick(p, imageDeps, { base, existing, max: caps.maxCount });
            if (res.ok) {
              added.push(res.attachment);
              existing = [...existing, res.attachment];
            } else refusals.push(res.message);
          } catch (e) {
            refusals.push(`${p.name} could not be read: ${e instanceof Error ? e.message : String(e)}`);
          }
        }
        if (added.length > 0) setPending((prev) => [...prev, ...added]);
        if (refusals.length > 0) setAttachNotice(refusals.join('\n'));
      } catch (e) {
        setAttachNotice(`Could not open the picker: ${e instanceof Error ? e.message : String(e)}`);
      } finally {
        picking.current = false;
      }
    },
    [caps.maxCount, chatId, draft, imageDeps, imageDims, sendProvider, taskId],
  );
  const handleRemoveAttachment = useCallback((id: string) => {
    setPending((prev) => prev.filter((a) => a.id !== id));
    setAttachNotice(null);
  }, []);

  const handleSend = useCallback(() => send('queue'), [send]);
  const handleSendNow = useCallback(() => send('interrupt'), [send]);

  const handleStop = useCallback(async () => {
    if (liveRunId === null || stopping) return;
    hapticFor('stop');
    setStopping(true);
    setSendError(null);
    const failure = await requestStop(conn, liveRunId);
    if (failure !== null && mounted.current) setSendError(failure);
    if (mounted.current) setStopping(false);
  }, [conn, liveRunId, stopping]);

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
      hapticFor('permission');
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

  const canSend = draft.trim().length > 0 && sendProvider !== null;
  const stopView = stopControlView(liveRunId, stopping);

  // The not-finished footer: same visibility rule as before, now the last transcript item.
  const failedRun = state.kind === 'ready' && state.run?.FailureKind === 'rate_limited' ? state.run : null;
  const failedRunId = failedRun?.ID ?? null;
  useEffect(() => {
    setSuccessor(null);
    if (failedRunId === null) return;
    let cancelled = false;
    listRunsForTask(conn, taskId)
      .then((runs) => {
        if (!cancelled) setSuccessor(findSuccessor(runs, failedRunId));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [conn, taskId, failedRunId, epoch]);
  const heldUntil = failedRun ? findHeldUntil(queue.items, failedRun.Prompt) : null;
  const heldUntilMs = heldUntil?.getTime() ?? null;
  const footer = useMemo(() => {
    if (state.kind !== 'ready' || state.run === null || continued || liveRunId !== null) return null;
    const view = describeRunFooter(state.run, new Date(), {
      heldUntil: heldUntilMs === null ? null : new Date(heldUntilMs),
      continuedOn: successor,
    });
    const continues = describeContinues(state.run);
    return view && continues && !view.note ? { ...view, note: continues } : view;
  }, [continued, heldUntilMs, liveRunId, state, successor]);
  const items = useMemo(() => buildTranscript(lines, permissions, footer), [lines, permissions, footer]);

  // Stick-to-bottom (vercel-react-native-skills: never track scroll position in useState per frame): `following` is a
  // ref written by onScroll; React state (`showJump`) changes only when the boolean flips.
  const listRef = useRef<FlatList<TranscriptItem>>(null);
  const following = useRef(true);
  const [showJump, setShowJump] = useState(false);
  const handleScroll = useCallback((e: NativeSyntheticEvent<NativeScrollEvent>) => {
    const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
    const next = nextFollowState(
      { following: following.current },
      { contentOffsetY: contentOffset.y, contentHeight: contentSize.height, layoutHeight: layoutMeasurement.height },
    );
    if (next.following !== following.current) {
      following.current = next.following;
      setShowJump(!next.following);
    }
  }, []);
  const handleContentSizeChange = useCallback(() => {
    if (following.current) listRef.current?.scrollToEnd({ animated: false });
  }, []);
  const handleJump = useCallback(() => {
    following.current = true;
    setShowJump(false);
    listRef.current?.scrollToEnd({ animated: true });
  }, []);

  // vercel-react-native-skills: stable keys (the first line's key), memoised item components that take ids/primitives
  // and stable callbacks. (RN's FlatList has no getItemType -- that is FlashList's, which this app does not depend on.)
  const renderItem = useCallback(
    ({ item }: ListRenderItemInfo<TranscriptItem>) => {
      switch (item.type) {
        case 'user':
          return <UserBubble text={item.text} attachments={item.attachments} loadThumb={loadThumb} />;
        case 'assistant':
          return <AssistantMessage text={item.text} />;
        case 'thinking':
          return <ThinkingRow text={item.text} />;
        case 'tool':
          return (
            <ToolCallRow
              id={item.key}
              toolName={item.toolName}
              title={item.title}
              status={item.status}
              expanded={expandedToolCalls.has(item.key)}
              onToggle={toggleExpanded}
            />
          );
        case 'meta':
          return <MetaLine text={item.text} />;
        case 'permission':
          return <PermissionCard request={item.request} onRespond={handlePermissionTap} />;
        case 'footer':
          return <FooterBanner sentence={item.sentence} note={item.note} waiting={item.waiting} canContinue={item.canContinue} onContinue={handleContinue} />;
      }
    },
    [expandedToolCalls, handleContinue, handlePermissionTap, loadThumb, toggleExpanded],
  );

  return (
    <View style={styles.screen}>
      {connState.kind === 'connecting' || connState.kind === 'reconnecting' ? <ReconnectingBanner>Reconnecting to the daemon…</ReconnectingBanner> : null}
      {state.kind === 'loading' ? <LoadingState /> : null}
      {state.kind === 'error' ? <EmptyState title="Couldn't load this task's runs." body={state.message} /> : null}
      {state.kind === 'ready' ? (
        <>
          {state.run !== null ? <RunStrip runId={state.run.ID} provider={state.run.Provider} status={state.run.Status} /> : null}
          <TaskTreeSection conn={conn} taskId={taskId} pendingTaskIds={noPendingTasks} onOpenTask={openTask} />
          <View style={styles.listWrap}>
            <FlatList
              ref={listRef}
              style={styles.list}
              contentContainerStyle={styles.listContent}
              data={items}
              keyExtractor={keyExtractor}
              renderItem={renderItem}
              ListEmptyComponent={state.run === null ? <ZeroRunEmptyState providers={providers} /> : emptyTranscript}
              contentInsetAdjustmentBehavior="automatic"
              onScroll={handleScroll}
              scrollEventThrottle={16}
              onContentSizeChange={handleContentSizeChange}
            />
            {showJump ? (
              <View style={styles.jumpWrap} pointerEvents="box-none">
                <UiHost>
                  <Button label="Jump to latest" variant="outlined" onPress={handleJump} style={styles.jumpButton} />
                </UiHost>
              </View>
            ) : null}
          </View>
          {/* keyboard-controller (expo-animation): the wrapper pads by the keyboard height, so the list shrinks above the dock. The dock's own padding (bottom inset)
              lives on an inner View: the animated paddingBottom of the avoiding view would override it. */}
          <KeyboardAvoidingView behavior="padding">
            <View style={styles.dock}>
              <Composer
                draftState={draftState}
                onChangeDraft={setDraft}
                liveRunId={liveRunId}
                stopView={stopView}
                canSend={canSend}
                sendError={sendError}
                queueItems={queue.items}
                onCancelQueued={(id) => void queue.cancel(id)}
                onStop={handleStop}
                onSend={handleSend}
                onSendNow={handleSendNow}
                placeholder={state.run === null ? 'Send the first prompt…' : undefined}
                attachments={pending}
                onRemoveAttachment={handleRemoveAttachment}
                attachMenu={attachMenu}
                onAttach={handleAttach}
                attachNotice={attachNotice}
              />
            </View>
          </KeyboardAvoidingView>
        </>
      ) : null}
    </View>
  );
}

// This screen has no unread-approval set of its own: the tree marks permissions from run state alone.
const noPendingTasks: ReadonlySet<number> = new Set();
const keyExtractor = (item: TranscriptItem) => item.key;
const emptyTranscript = <EmptyState title="(empty transcript)" body="Nothing has been said in this run yet." />;
/** A task with no run: either no providers to send with, or ready for its first prompt. */
function ZeroRunEmptyState({ providers }: { providers: ProviderInfo[] | null }) {
  if (providers !== null && providers.length === 0) return <EmptyState title="This daemon has no providers." body="Add one to start a run." />;
  return <EmptyState title="No prompts yet" body="Send the first prompt to start a run." />;
}

/** Shown by the task route when its `id` param is not a positive integer (ADR-0036 decision 4). */
export function TaskLoadError() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.screen}>
      <EmptyState title="Couldn't load this task's runs." body="This task link is not valid." />
    </View>
  );
}

function makeStyles(theme: AppTheme, bottomInset = 0) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: theme.background,
    },
    listWrap: { flex: 1 },
    list: { flex: 1 },
    listContent: {
      gap: theme.spacing[3],
      paddingHorizontal: theme.spacing[4],
      paddingVertical: theme.spacing[4],
    },
    dock: {
      backgroundColor: theme.background,
      paddingHorizontal: theme.spacing[3],
      paddingTop: theme.spacing[2],
      paddingBottom: Math.max(bottomInset, theme.spacing[2]),
    },
    jumpWrap: {
      position: 'absolute',
      left: 0,
      right: 0,
      bottom: theme.spacing[3],
      alignItems: 'center',
    },
    jumpButton: { height: 44 },
  });
}
