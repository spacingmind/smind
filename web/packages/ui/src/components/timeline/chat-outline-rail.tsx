import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
} from "react";

import { cn } from "@/lib/utils";
import {
  promptTickMagnification,
  type ActivePromptSource,
  type OutlinePrompt,
} from "./chat-outline";
import { createChatOutlineHoverIntent } from "./hover-intent";

// Hover tracking lives on the rail and the slots, never on the button inside them:
// magnifying a pill must not move the box the pointer is resting on.
const RESTING_PILL_WIDTH = 10;
const ACTIVE_PILL_WIDTH = 18;
const MAGNIFIED_PILL_WIDTH = 26;
const RESTING_PILL_HEIGHT = 2;
const MAGNIFIED_PILL_HEIGHT = 4;

export interface ChatOutlineRailProps {
  prompts: readonly OutlinePrompt[];
  activePrompt: ActivePromptSource;
  onJump: (runId: string) => void;
  /** Instant jump used while dragging across the rail; falls back to `onJump`. */
  onScrub?: (runId: string) => void;
  /** Test probe: called each time a tick renders. */
  onTickRender?: (runId: string) => void;
}

export const ChatOutlineRail = memo(function ChatOutlineRail({
  prompts,
  activePrompt,
  onJump,
  onScrub,
  onTickRender,
}: ChatOutlineRailProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null);
  const activeId = useSyncExternalStore(activePrompt.subscribe, activePrompt.getActiveId);
  const railRef = useRef<HTMLDivElement>(null);
  const pressedRef = useRef(false);
  const lastScrubRef = useRef<number | null>(null);

  const hoverIntent = useMemo(
    () =>
      createChatOutlineHoverIntent({
        activate: setHoveredIndex,
        schedule: (callback, delayMs) => window.setTimeout(callback, delayMs),
        cancel: (timerId) => window.clearTimeout(timerId),
      }),
    [],
  );
  useEffect(() => () => hoverIntent.dispose(), [hoverIntent]);

  const promptsRef = useRef(prompts);
  promptsRef.current = prompts;

  const hoverTick = useCallback((index: number) => hoverIntent.pointAt(index), [hoverIntent]);
  const handleFocusChange = useCallback((index: number, focused: boolean) => {
    setFocusedIndex((current) => (focused ? index : current === index ? null : current));
  }, []);

  // Slots are equal-height and centred, so together they can be shorter than the rail; map the
  // pointer over the slots' own extent, not the rail's.
  const indexAtPointer = useCallback((clientY: number): number | null => {
    const slots = railRef.current?.children;
    const count = promptsRef.current.length;
    if (!slots || slots.length === 0 || count === 0) return null;
    const top = slots[0].getBoundingClientRect().top;
    const height = slots[slots.length - 1].getBoundingClientRect().bottom - top;
    if (height <= 0) return null;
    const raw = Math.floor(((clientY - top) / height) * count);
    return Math.min(count - 1, Math.max(0, raw));
  }, []);

  const handlePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    const index = indexAtPointer(event.clientY);
    if (index === null) return;
    pressedRef.current = true;
    lastScrubRef.current = index;
    try {
      event.currentTarget.setPointerCapture?.(event.pointerId);
    } catch {
      // Capture is best-effort; dragging still works while the pointer stays on the rail.
    }
    onJump(promptsRef.current[index].id);
  };
  const handlePointerMove = (event: PointerEvent<HTMLDivElement>) => {
    hoverIntent.move({ x: event.clientX, y: event.clientY });
    if (!pressedRef.current) return;
    const index = indexAtPointer(event.clientY);
    if (index === null || index === lastScrubRef.current) return;
    lastScrubRef.current = index;
    (onScrub ?? onJump)(promptsRef.current[index].id);
  };
  const endPress = () => {
    pressedRef.current = false;
    lastScrubRef.current = null;
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const count = promptsRef.current.length;
    const current = focusedIndex ?? 0;
    let next: number;
    if (event.key === "ArrowDown") next = Math.min(count - 1, current + 1);
    else if (event.key === "ArrowUp") next = Math.max(0, current - 1);
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = count - 1;
    else return;
    event.preventDefault();
    railRef.current?.querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus();
  };

  if (prompts.length < 2) return null;

  // The pointer owns the magnified band while it is on the rail; keyboard focus drives the
  // same band and preview once the pointer leaves.
  const attentionIndex = hoveredIndex ?? focusedIndex;
  const activeIndex = prompts.findIndex((prompt) => prompt.id === activeId);
  const tabbableIndex = activeIndex === -1 ? 0 : activeIndex;

  return (
    <div
      ref={railRef}
      role="tablist"
      aria-orientation="vertical"
      aria-label="Chat timeline"
      data-testid="chat-outline-rail"
      className="absolute top-[10%] bottom-[10%] left-2 z-[5] flex w-9 touch-none flex-col items-center justify-center select-none"
      onPointerEnter={(event) => hoverIntent.enter({ x: event.clientX, y: event.clientY })}
      onPointerMove={handlePointerMove}
      onPointerLeave={() => hoverIntent.leave()}
      onPointerDown={handlePointerDown}
      onPointerUp={endPress}
      onPointerCancel={endPress}
      onKeyDown={handleKeyDown}
    >
      {prompts.map((prompt, index) => (
        <ChatOutlineTick
          key={prompt.id}
          index={index}
          runId={prompt.id}
          preview={prompt.preview}
          label={`${index + 1} of ${prompts.length}: ${prompt.preview}`}
          isActive={index === activeIndex}
          tabbable={index === tabbableIndex}
          hasAttention={index === attentionIndex}
          magnification={
            attentionIndex === null ? 0 : promptTickMagnification(index - attentionIndex)
          }
          onHover={hoverTick}
          onFocusChange={handleFocusChange}
          onJump={onJump}
          onRender={onTickRender}
        />
      ))}
    </div>
  );
});

interface ChatOutlineTickProps {
  index: number;
  runId: string;
  preview: string;
  label: string;
  isActive: boolean;
  tabbable: boolean;
  hasAttention: boolean;
  magnification: number;
  onHover: (index: number) => void;
  onFocusChange: (index: number, focused: boolean) => void;
  onJump: (runId: string) => void;
  onRender?: (runId: string) => void;
}

const ChatOutlineTick = memo(function ChatOutlineTick({
  index,
  runId,
  preview,
  label,
  isActive,
  tabbable,
  hasAttention,
  magnification,
  onHover,
  onFocusChange,
  onJump,
  onRender,
}: ChatOutlineTickProps) {
  onRender?.(runId);

  // A pointer press already jumped on pointerdown; only keyboard-originated clicks
  // (Enter/Space, detail === 0) act here.
  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    if (event.detail === 0) onJump(runId);
  };

  const restingWidth = isActive ? ACTIVE_PILL_WIDTH : RESTING_PILL_WIDTH;
  const pillWidth = restingWidth + magnification * (MAGNIFIED_PILL_WIDTH - restingWidth);
  const pillHeight =
    RESTING_PILL_HEIGHT + magnification * (MAGNIFIED_PILL_HEIGHT - RESTING_PILL_HEIGHT);

  // Slots tile the rail with no gaps (flex-shrink), so a long chat still fits. Pills share
  // one left edge and grow rightward, so magnification reads as a travelling bulge.
  return (
    <div
      className="relative flex min-h-0 w-full flex-[0_1_8px] items-center"
      onPointerEnter={() => onHover(index)}
    >
      <button
        type="button"
        role="tab"
        aria-selected={isActive}
        aria-label={label}
        tabIndex={tabbable ? 0 : -1}
        data-testid={`chat-outline-tick-${runId}`}
        onClick={handleClick}
        onFocus={() => onFocusChange(index, true)}
        onBlur={() => onFocusChange(index, false)}
        className="flex h-full w-full cursor-pointer items-center rounded-sm pl-1 outline-none focus-visible:ring-1 focus-visible:ring-border-hover"
      >
        <span
          aria-hidden
          style={{ width: pillWidth, height: pillHeight }}
          className={cn(
            "block rounded-full transition-[width,height,background-color] duration-150 ease-out",
            hasAttention ? "bg-foreground" : isActive ? "bg-foreground-muted" : "bg-border",
          )}
        />
      </button>
      {hasAttention && (
        <div
          aria-hidden
          data-testid="chat-outline-preview"
          className="pointer-events-none absolute top-1/2 left-[40px] z-10 flex min-h-12 w-[260px] -translate-y-1/2 items-center rounded-lg bg-popover px-3 py-2 text-ui-sm text-popover-foreground shadow-md ring-1 ring-border"
        >
          <span className="line-clamp-2 break-words">{preview}</span>
        </div>
      )}
    </div>
  );
});
