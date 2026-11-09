// useForkView: what the task screen shows for a forked chat (ADR-0057 PR 1b AC20-AC21) and the turn the fork
// action names. One chat.list gives this chat's own fork record and the parent's title; the parent's run LIST (cheap)
// gives the banner's turn number; the parent's turns themselves (run.logs) are fetched only when the block is
// expanded, and a seeded fork's summary (chat.forkSeed) likewise. A chat that is not a fork loads nothing extra beyond
// its own run list, which the fork action's turn number needs.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { ChatInfo, forkSeed, listChats, listRunsForTask, RunSummary } from './api';
import { forkBannerText, forkDividerLabel, forkTurnNumber, inheritedUpTo } from './chatFork';
import type { ForkTurn } from './screens/task/ForkContext';
import { RunLogsResult } from './historySource';
import type { RelayConnection } from './relay/RelayConnection';
import { timelineFromLogs } from './runTimeline';

export interface ForkView {
  /** This chat's title, for the fork confirm; "" until loaded. */
  chatTitle: string;
  /** The 1-based turn the latest run is among this chat's runs (the fork action's number); null until known. */
  forkTurn: number | null;
  /** Present only for a forked chat. */
  fork: null | {
    mode: 'native' | 'seeded';
    bannerText: string;
    dividerLabel: string;
    parentChatId: number;
    /** The parent chat is archived or missing: no "View original". */
    parentGone: boolean;
    parentTitle: string | null;
    turnCount: number;
    turns: ForkTurn[] | null;
    loadingTurns: boolean;
    expanded: boolean;
    seed: string | null;
    seedFailed: boolean;
  };
  toggle: () => void;
}

/** The assistant's text of one run's logs: its chunk lines joined. */
function answerOf(logs: RunLogsResult): string | null {
  const text = timelineFromLogs({ runId: undefined, events: (logs.events ?? []) as never[] })
    .filter((l) => l.role === 'assistant')
    .map((l) => l.text)
    .join('');
  return text === '' ? null : text;
}

export function useForkView(conn: RelayConnection, taskId: number, chatId: number | null, latestRunId: string | null): ForkView {
  const [chats, setChats] = useState<ChatInfo[] | null>(null);
  const [ownRuns, setOwnRuns] = useState<RunSummary[] | null>(null);
  const [parentRuns, setParentRuns] = useState<RunSummary[] | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [turns, setTurns] = useState<ForkTurn[] | null>(null);
  const [loadingTurns, setLoadingTurns] = useState(false);
  const [seed, setSeed] = useState<string | null>(null);
  const [seedFailed, setSeedFailed] = useState(false);

  useEffect(() => {
    setChats(null);
    setOwnRuns(null);
    setParentRuns(null);
    setExpanded(false);
    setTurns(null);
    setSeed(null);
    setSeedFailed(false);
    if (chatId === null) return;
    let alive = true;
    listChats(conn, taskId)
      .then((list) => alive && setChats(list))
      .catch(() => alive && setChats([]));
    return () => {
      alive = false;
    };
  }, [conn, taskId, chatId]);

  // This chat's own runs: the fork action's turn number. Reloaded when the latest run changes (a new turn).
  useEffect(() => {
    if (chatId === null) return;
    let alive = true;
    listRunsForTask(conn, taskId, chatId)
      .then((runs) => alive && setOwnRuns(runs))
      .catch(() => alive && setOwnRuns([]));
    return () => {
      alive = false;
    };
  }, [conn, taskId, chatId, latestRunId]);

  const chat = useMemo(() => chats?.find((c) => c.ID === chatId) ?? null, [chats, chatId]);
  const forkInfo = chat?.fork ?? null;
  const parentChatId = forkInfo?.parentChatId ?? null;
  const parent = useMemo(() => (parentChatId === null ? null : (chats?.find((c) => c.ID === parentChatId) ?? null)), [chats, parentChatId]);

  useEffect(() => {
    if (parentChatId === null) return;
    let alive = true;
    listRunsForTask(conn, taskId, parentChatId)
      .then((runs) => alive && setParentRuns(runs))
      .catch(() => alive && setParentRuns([]));
    return () => {
      alive = false;
    };
  }, [conn, taskId, parentChatId]);

  const inherited = useMemo(() => (forkInfo && parentRuns ? inheritedUpTo(parentRuns, forkInfo.forkedAtRunId) : null), [forkInfo, parentRuns]);
  const mode = forkInfo?.mode ?? null;
  const forkedAt = forkInfo?.forkedAtRunId ?? null;

  // Expanding loads: a native fork's parent turns (run.logs each, oldest first), a seeded fork's summary.
  useEffect(() => {
    if (!expanded || chatId === null) return;
    let alive = true;
    if (mode === 'native' && inherited !== null && turns === null) {
      setLoadingTurns(true);
      Promise.all(
        inherited.map(async (run): Promise<ForkTurn> => {
          try {
            const logs = (await conn.call('run.logs', { runId: run.ID, detail: 'auto' })) as RunLogsResult;
            return { key: run.ID, prompt: run.Prompt, answer: answerOf(logs) };
          } catch {
            return { key: run.ID, prompt: run.Prompt, answer: null };
          }
        }),
      ).then((list) => {
        if (!alive) return;
        setTurns(list);
        setLoadingTurns(false);
      });
    }
    if (mode === 'seeded' && seed === null && !seedFailed) {
      forkSeed(conn, chatId)
        .then((s) => alive && setSeed(s))
        .catch(() => alive && setSeedFailed(true));
    }
    return () => {
      alive = false;
    };
  }, [conn, expanded, chatId, mode, forkedAt, inherited, turns, seed, seedFailed]);

  const toggle = useCallback(() => setExpanded((v) => !v), []);
  const forkTurn = latestRunId !== null && ownRuns !== null ? forkTurnNumber(ownRuns, latestRunId) : null;
  const parentGone = parent === null || parent.ArchivedAt !== null;

  return {
    chatTitle: chat?.Title ?? '',
    forkTurn,
    fork:
      forkInfo === null
        ? null
        : {
            mode: forkInfo.mode,
            bannerText: forkBannerText(forkInfo, parent && parent.ArchivedAt === null ? parent.Title : null, inherited !== null && inherited.length > 0 ? inherited.length : null),
            dividerLabel: forkDividerLabel(forkInfo),
            parentChatId: forkInfo.parentChatId,
            parentGone,
            parentTitle: parent?.Title ?? null,
            turnCount: inherited?.length ?? 0,
            turns,
            loadingTurns,
            expanded,
            seed,
            seedFailed,
          },
    toggle,
  };
}
