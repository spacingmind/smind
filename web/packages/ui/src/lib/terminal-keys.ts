/**
 * Terminal key encoder, a port of Paseo's `encodeTerminalKeyInput`
 * (packages/protocol/src/terminal-key-input.ts) and the pending-modifier data mapping from
 * packages/app/src/utils/terminal-keys.ts. The win32 / kitty / function-key branches are omitted: the
 * touch key bar has no such keys.
 */

export interface TerminalKeyInput {
  key: string;
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
  meta?: boolean;
}

export interface TerminalInputMode {
  applicationCursorKeys?: boolean;
}

export interface PendingTerminalModifiers {
  ctrl: boolean;
  shift: boolean;
  alt: boolean;
}

export function hasPendingModifiers(modifiers: PendingTerminalModifiers): boolean {
  return modifiers.ctrl || modifiers.shift || modifiers.alt;
}

function modifierParam(input: TerminalKeyInput): number {
  let value = 1;
  if (input.shift) value += 1;
  if (input.alt) value += 2;
  if (input.ctrl) value += 4;
  if (input.meta) value += 8;
  return value;
}

function withAltPrefix(sequence: string, input: TerminalKeyInput): string {
  return input.alt ? `\x1b${sequence}` : sequence;
}

function ctrlSymbolCode(char: string): string | null {
  switch (char) {
    case " ":
    case "@":
    case "2":
      return "\x00";
    case "[":
    case "3":
      return "\x1b";
    case "\\":
    case "4":
      return "\x1c";
    case "]":
    case "5":
      return "\x1d";
    case "^":
    case "6":
      return "\x1e";
    case "_":
    case "/":
    case "7":
      return "\x1f";
    case "8":
    case "?":
      return "\x7f";
    default:
      return null;
  }
}

function encodeCtrlChar(char: string, input: TerminalKeyInput): string {
  const upper = char.toUpperCase();
  if (upper.length === 1 && upper >= "A" && upper <= "Z") {
    return withAltPrefix(String.fromCharCode(upper.charCodeAt(0) - 64), input);
  }
  const symbol = ctrlSymbolCode(char);
  if (symbol !== null) return withAltPrefix(symbol, input);
  if (char.length === 1) return withAltPrefix(String.fromCharCode(char.charCodeAt(0) & 0x1f), input);
  return withAltPrefix(char, input);
}

function encodePrintableKey(input: TerminalKeyInput): string {
  const char = input.shift ? input.key.toUpperCase() : input.key;
  if (input.ctrl) return encodeCtrlChar(char, input);
  return withAltPrefix(char, input);
}

function csiWithModifier(finalByte: string, input: TerminalKeyInput): string {
  const mod = modifierParam(input);
  return mod === 1 ? `\x1b[${finalByte}` : `\x1b[1;${mod}${finalByte}`;
}

function csiTilde(base: number, input: TerminalKeyInput): string {
  const mod = modifierParam(input);
  return mod === 1 ? `\x1b[${base}~` : `\x1b[${base};${mod}~`;
}

function encodeArrowKey(finalByte: string, input: TerminalKeyInput, mode: TerminalInputMode): string {
  if (mode.applicationCursorKeys) {
    return modifierParam(input) === 1 ? `\x1bO${finalByte}` : csiWithModifier(finalByte, input);
  }
  return csiWithModifier(finalByte, input);
}

function encodeNavigationKey(key: string, input: TerminalKeyInput, mode: TerminalInputMode): string | null {
  switch (key) {
    case "ArrowUp":
      return encodeArrowKey("A", input, mode);
    case "ArrowDown":
      return encodeArrowKey("B", input, mode);
    case "ArrowRight":
      return encodeArrowKey("C", input, mode);
    case "ArrowLeft":
      return encodeArrowKey("D", input, mode);
    case "Home":
      return csiWithModifier("H", input);
    case "End":
      return csiWithModifier("F", input);
    case "Insert":
      return csiTilde(2, input);
    case "Delete":
      return csiTilde(3, input);
    case "PageUp":
      return csiTilde(5, input);
    case "PageDown":
      return csiTilde(6, input);
    default:
      return null;
  }
}

export function encodeTerminalKey(input: TerminalKeyInput, mode: TerminalInputMode = {}): string {
  const key = input.key;
  if (!key) return "";
  if (key.length === 1) return encodePrintableKey(input);

  switch (key) {
    case "Enter":
      return "\r";
    case "Tab":
      if (input.shift && !input.ctrl && !input.alt && !input.meta) return "\x1b[Z";
      return withAltPrefix("\t", input);
    case "Backspace":
      return withAltPrefix("\x7f", input);
    case "Escape":
      return "\x1b";
    default:
      return encodeNavigationKey(key, input, mode) ?? "";
  }
}

/** Paseo's normalizeTerminalTransportKey: single-character keys travel lowercase, Shift re-uppercases them while encoding. */
export function terminalKeyInput(key: string, modifiers: PendingTerminalModifiers): TerminalKeyInput {
  return {
    key: key.length === 1 ? key.toLowerCase() : key,
    ctrl: modifiers.ctrl,
    alt: modifiers.alt,
    shift: modifiers.shift,
  };
}

export type PendingModifierData = { mode: "key"; key: string; clearPendingModifiers: boolean } | { mode: "raw"; clearPendingModifiers: boolean };

function mapTerminalDataToKey(data: string): string | null {
  if (data === "\r" || data === "\n") return "Enter";
  if (data === "\t") return "Tab";
  if (data === "\x7f" || data === "\b") return "Backspace";
  if (data === "\x1b") return "Escape";
  if (data.length === 1) {
    const code = data.charCodeAt(0);
    if (code >= 0x20 && code <= 0x7e) return data;
  }
  return null;
}

/** What to do with OS-keyboard `data` while a sticky modifier is armed: re-encode a single mappable key with the modifiers, otherwise send it raw; the modifiers clear either way. */
export function resolvePendingModifierData({
  data,
  pending,
}: {
  data: string;
  pending: PendingTerminalModifiers;
}): PendingModifierData {
  const clearPendingModifiers = hasPendingModifiers(pending);
  const key = clearPendingModifiers ? mapTerminalDataToKey(data) : null;
  return key === null ? { mode: "raw", clearPendingModifiers } : { mode: "key", key, clearPendingModifiers };
}
