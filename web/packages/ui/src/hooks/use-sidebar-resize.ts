import { useCallback, useEffect, useRef } from "react";
import type { KeyboardEvent, PointerEvent, RefObject } from "react";

import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH } from "@/hooks/use-sidebar-width";

/** Releasing a drag whose raw width is below this collapses the sidebar to the icon rail (spec D7). */
export const SIDEBAR_SNAP_WIDTH = 144;
/** Arrow-key step on the focused handle, in pixels. */
export const SIDEBAR_KEYBOARD_STEP = 16;

/** Never let the sidebar take more than half of the shell, whatever the max says. */
const MAX_SHELL_FRACTION = 0.5;
const DRAGGING_BODY_CLASSES = ["cursor-col-resize", "select-none"];

type UseSidebarResizeOptions = {
  /** The sidebar panel element; gets `data-resizing` while dragging. Its parent is the shell, its nearest sidebar wrapper receives `--sidebar-width`. */
  panelRef: RefObject<HTMLElement | null>;
  /** Committed (persisted) width in px. */
  width: number;
  /** Called once per gesture with the clamped width. */
  onCommit: (width: number) => void;
  /** Called when a drag is released below SIDEBAR_SNAP_WIDTH. */
  onCollapse: () => void;
};

/**
 * ZCode-style sidebar drag: during a drag nothing goes through React -- the
 * live width is written straight onto the SidebarProvider wrapper's
 * `--sidebar-width` (rAF-coalesced) and the panel is flagged with
 * `data-resizing` so its width transition is off. State + localStorage are
 * committed once on release (or per keypress).
 */
export function useSidebarResize({ panelRef, width, onCommit, onCollapse }: UseSidebarResizeOptions) {
  const drag = useRef<{ startX: number; startWidth: number; raw: number } | null>(null);
  const frame = useRef<number | null>(null);
  // Latest props, so the stable handlers below never close over stale ones.
  const latest = useRef({ width, onCommit, onCollapse });
  latest.current = { width, onCommit, onCollapse };

  const wrapper = useCallback(
    () => panelRef.current?.closest<HTMLElement>('[data-slot="sidebar-wrapper"]') ?? null,
    [panelRef],
  );

  const maxWidth = useCallback(() => {
    const shell = panelRef.current?.parentElement?.getBoundingClientRect().width ?? 0;
    return shell > 0 ? Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, shell * MAX_SHELL_FRACTION)) : SIDEBAR_MAX_WIDTH;
  }, [panelRef]);

  const clampWidth = useCallback(
    (raw: number) => Math.min(maxWidth(), Math.max(SIDEBAR_MIN_WIDTH, raw)),
    [maxWidth],
  );

  const cancelFrame = () => {
    if (frame.current !== null) {
      cancelAnimationFrame(frame.current);
      frame.current = null;
    }
  };

  const setDragging = useCallback(
    (active: boolean) => {
      const panel = panelRef.current;
      if (panel) {
        if (active) panel.dataset.resizing = "true";
        else delete panel.dataset.resizing;
      }
      document.body.classList.toggle("sidebar-resizing", active);
      for (const cls of DRAGGING_BODY_CLASSES) document.body.classList.toggle(cls, active);
    },
    [panelRef],
  );

  const writeVar = useCallback(
    (px: number) => {
      wrapper()?.style.setProperty("--sidebar-width", `${px}px`);
    },
    [wrapper],
  );

  const finish = useCallback(
    (commit: boolean, clientX?: number) => {
      const current = drag.current;
      if (!current) return;
      drag.current = null;
      cancelFrame();
      setDragging(false);
      const committed = latest.current.width;
      if (!commit) {
        writeVar(committed);
        return;
      }
      if (clientX !== undefined) current.raw = current.startWidth + (clientX - current.startX);
      if (current.raw < SIDEBAR_SNAP_WIDTH) {
        // Keep the committed width so re-expanding returns to it.
        writeVar(committed);
        latest.current.onCollapse();
        return;
      }
      const next = clampWidth(current.raw);
      writeVar(next);
      latest.current.onCommit(next);
    },
    [clampWidth, setDragging, writeVar],
  );

  useEffect(
    () => () => {
      if (drag.current) {
        drag.current = null;
        writeVar(latest.current.width);
      }
      cancelFrame();
      setDragging(false);
    },
    [setDragging, writeVar],
  );

  const onPointerDown = useCallback(
    (event: PointerEvent<HTMLElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      try {
        event.currentTarget.setPointerCapture?.(event.pointerId);
      } catch {
        // The pointer is already gone; the drag simply won't track.
      }
      drag.current = { startX: event.clientX, startWidth: latest.current.width, raw: latest.current.width };
      setDragging(true);
    },
    [setDragging],
  );

  const onPointerMove = useCallback(
    (event: PointerEvent<HTMLElement>) => {
      const current = drag.current;
      if (!current) return;
      current.raw = current.startWidth + (event.clientX - current.startX);
      if (frame.current !== null) return;
      frame.current = requestAnimationFrame(() => {
        frame.current = null;
        if (drag.current) writeVar(clampWidth(drag.current.raw));
      });
    },
    [clampWidth, writeVar],
  );

  const onPointerUp = useCallback(
    (event: PointerEvent<HTMLElement>) => {
      if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
        event.currentTarget.releasePointerCapture?.(event.pointerId);
      }
      finish(true, event.clientX);
    },
    [finish],
  );

  const onPointerCancel = useCallback(() => finish(false), [finish]);

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLElement>) => {
      const current = latest.current.width;
      let next: number;
      switch (event.key) {
        case "ArrowLeft":
          next = current - SIDEBAR_KEYBOARD_STEP;
          break;
        case "ArrowRight":
          next = current + SIDEBAR_KEYBOARD_STEP;
          break;
        case "Home":
          next = SIDEBAR_MIN_WIDTH;
          break;
        case "End":
          next = maxWidth();
          break;
        default:
          return;
      }
      event.preventDefault();
      latest.current.onCommit(clampWidth(next));
    },
    [clampWidth, maxWidth],
  );

  return { onPointerDown, onPointerMove, onPointerUp, onPointerCancel, onKeyDown };
}
