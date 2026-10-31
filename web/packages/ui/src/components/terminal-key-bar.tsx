import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";

import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

export type TerminalModifier = "ctrl" | "shift" | "alt";

type KeyId =
  | "esc"
  | "tab"
  | "up"
  | "down"
  | "left"
  | "right"
  | "enter"
  | TerminalModifier
  | "copy"
  | "paste"
  | "keyboard";

/**
 * Port of Paseo's mobile terminal key bar layouts (terminal-virtual-keyboard.ts): wide (>= 660) is one
 * row, narrow (< 300) is rows of three, anything else (and an unmeasured bar) is two rows. Copy is this
 * port's one addition, placed right before Paste.
 */
export function resolveKeyBarRows(width: number): KeyId[][] {
  if (width >= 660) {
    return [["esc", "tab", "ctrl", "alt", "shift", "left", "down", "up", "right", "copy", "paste", "keyboard", "enter"]];
  }
  if (width > 0 && width < 300) {
    return [
      ["esc", "tab", "ctrl"],
      ["alt", "shift", "keyboard"],
      ["copy", "paste", "enter"],
      ["left", "down", "up", "right"],
    ];
  }
  return [
    ["esc", "tab", "ctrl", "up", "shift", "keyboard"],
    ["alt", "copy", "paste", "left", "down", "right", "enter"],
  ];
}

const DOM_KEYS: Partial<Record<KeyId, string>> = {
  esc: "Escape",
  tab: "Tab",
  up: "ArrowUp",
  down: "ArrowDown",
  left: "ArrowLeft",
  right: "ArrowRight",
  enter: "Enter",
};

const LABELS: Partial<Record<KeyId, string>> = {
  esc: "Esc",
  tab: "Tab",
  ctrl: "Ctrl",
  shift: "Shift",
  alt: "Alt",
  enter: "Enter",
  up: "↑",
  down: "↓",
  left: "←",
  right: "→",
};

const ARIA_LABELS: Partial<Record<KeyId, string>> = {
  up: "Arrow up",
  down: "Arrow down",
  left: "Arrow left",
  right: "Arrow right",
  keyboard: "Toggle keyboard",
};

const KEY_CLASS =
  "flex h-[34px] min-w-0 flex-1 items-center justify-center gap-1 rounded-md border border-border bg-muted text-ui-base font-medium text-foreground-muted hover:bg-muted/70 active:bg-muted/70 disabled:opacity-50";

export function TerminalKeyBar({
  modifiers,
  onToggleModifier,
  onKey,
  onCopy,
  onPaste,
  onToggleKeyboard,
  copyDisabled,
  pasteDisabled,
  style,
}: {
  modifiers: Record<TerminalModifier, boolean>;
  onToggleModifier: (modifier: TerminalModifier) => void;
  onKey: (domKey: string) => void;
  onCopy: () => void;
  onPaste: () => void;
  onToggleKeyboard: () => void;
  copyDisabled?: boolean;
  pasteDisabled?: boolean;
  style?: CSSProperties;
}) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const root = rootRef.current;
    if (!root || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver((entries) => {
      const next = entries[0]?.contentRect.width;
      if (next !== undefined) setWidth(next);
    });
    observer.observe(root);
    return () => observer.disconnect();
  }, []);

  function renderKey(id: KeyId): ReactNode {
    // Pressing a key must not move focus off the xterm textarea, or the on-screen keyboard dismisses.
    const keepFocus = {
      onMouseDown: (e: React.MouseEvent) => e.preventDefault(),
      onPointerDown: (e: React.PointerEvent) => e.preventDefault(),
    };
    if (id === "ctrl" || id === "shift" || id === "alt") {
      const active = modifiers[id];
      return (
        <button
          key={id}
          type="button"
          data-testid={`terminal-key-${id}`}
          aria-pressed={active}
          className={cn(KEY_CLASS, active && "border-primary text-foreground")}
          onClick={() => onToggleModifier(id)}
          {...keepFocus}
        >
          {LABELS[id]}
        </button>
      );
    }
    if (id === "copy") {
      return (
        <button key={id} type="button" data-testid="terminal-copy" className={KEY_CLASS} disabled={copyDisabled} onClick={onCopy} {...keepFocus}>
          Copy
        </button>
      );
    }
    if (id === "paste") {
      return (
        <button key={id} type="button" data-testid="terminal-paste" className={KEY_CLASS} disabled={pasteDisabled} onClick={onPaste} {...keepFocus}>
          Paste
        </button>
      );
    }
    if (id === "keyboard") {
      return (
        <button
          key={id}
          type="button"
          data-testid="terminal-keyboard-toggle"
          aria-label={ARIA_LABELS.keyboard}
          className={KEY_CLASS}
          onClick={onToggleKeyboard}
          {...keepFocus}
        >
          <Icons.shortcuts className="size-4" />
        </button>
      );
    }
    return (
      <button
        key={id}
        type="button"
        data-testid={`terminal-key-${id}`}
        aria-label={ARIA_LABELS[id]}
        className={KEY_CLASS}
        onClick={() => onKey(DOM_KEYS[id]!)}
        {...keepFocus}
      >
        {LABELS[id]}
      </button>
    );
  }

  return (
    <div
      ref={rootRef}
      data-testid="terminal-touch-actions"
      className="hidden shrink-0 border-t bg-background px-2 py-2 pointer-coarse:block"
      style={style}
    >
      <div className="mx-auto flex w-full max-w-[880px] flex-col gap-1">
        {resolveKeyBarRows(width).map((row, index) => (
          <div key={index} className="flex items-center gap-1">
            {row.map(renderKey)}
          </div>
        ))}
      </div>
    </div>
  );
}
