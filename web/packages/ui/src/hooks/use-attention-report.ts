import { useEffect, useRef } from "react";

import { isComposerInput } from "@/lib/composer-focus";
import { desktop, isDesktop, type AttentionFocusReport } from "@/lib/platform";

/** Rapid state changes collapse into one trailing report (DT5 C AC9). */
const DEBOUNCE_MS = 100;

const noop = (): void => {};

/**
 * Reports the app's attention state (which task is in front, whether the
 * window is focused, whether a prompt box has focus) to the desktop shell
 * (`attention_focus_report`), so the shell can decide when a notification
 * banner may show (DT5 C AC9). A no-op in the browser build: it never
 * touches `desktop` there.
 *
 * Best-effort by design: a report that the shell rejects is swallowed,
 * and a report is sent only when the tuple differs from the last one sent.
 */
export function useAttentionReport(taskId: number | null): void {
  const taskIdRef = useRef(taskId);
  taskIdRef.current = taskId;
  const scheduleRef = useRef<() => void>(noop);

  useEffect(() => {
    if (!isDesktop) return;

    let windowFocused = document.hasFocus();
    let composerFocused = isComposerInput(document.activeElement);
    let lastSent = "";
    let timer: ReturnType<typeof setTimeout> | null = null;

    const current = (): AttentionFocusReport => ({ taskId: taskIdRef.current, windowFocused, composerFocused });
    const key = (report: AttentionFocusReport): string =>
      `${report.taskId}:${report.windowFocused}:${report.composerFocused}`;
    const send = (report: AttentionFocusReport): void => {
      lastSent = key(report);
      desktop.attentionFocusReport(report).catch(() => {});
    };
    const flush = (): void => {
      timer = null;
      const report = current();
      if (key(report) !== lastSent) send(report);
    };
    const schedule = (): void => {
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(flush, DEBOUNCE_MS);
    };

    // The initial state goes out at once, not through the debounce.
    send(current());

    const onWindowFocus = (): void => {
      windowFocused = true;
      schedule();
    };
    const onWindowBlur = (): void => {
      windowFocused = false;
      schedule();
    };
    const onVisibilityChange = (): void => {
      windowFocused = document.hidden ? false : document.hasFocus();
      schedule();
    };
    const onFocusIn = (e: FocusEvent): void => {
      composerFocused = isComposerInput(e.target as Element | null);
      schedule();
    };
    const onFocusOut = (): void => {
      composerFocused = false;
      schedule();
    };
    const onPageHide = (): void => {
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
      send({ taskId: taskIdRef.current, windowFocused: false, composerFocused: false });
    };

    window.addEventListener("focus", onWindowFocus);
    window.addEventListener("blur", onWindowBlur);
    document.addEventListener("visibilitychange", onVisibilityChange);
    document.addEventListener("focusin", onFocusIn);
    document.addEventListener("focusout", onFocusOut);
    window.addEventListener("pagehide", onPageHide);
    scheduleRef.current = schedule;

    return () => {
      scheduleRef.current = noop;
      window.removeEventListener("focus", onWindowFocus);
      window.removeEventListener("blur", onWindowBlur);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      document.removeEventListener("focusin", onFocusIn);
      document.removeEventListener("focusout", onFocusOut);
      window.removeEventListener("pagehide", onPageHide);
      if (timer !== null) clearTimeout(timer);
    };
  }, []);

  // A task switch is a state change like any other: schedule it through the
  // debounce rather than re-arming the listeners.
  useEffect(() => {
    if (isDesktop) scheduleRef.current();
  }, [taskId]);
}
