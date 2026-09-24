/**
 * Combo strings: the one textual spelling a shortcut has, everywhere.
 *
 * A combo is written `Mod+Alt+T` / `Shift+?` / `Escape` and parsed once into
 * a {@link KeyCombo} that a real `KeyboardEvent` is matched against. The
 * shape is smind's scaled-down take on
 * `refs/paseo/packages/app/src/keyboard/shortcut-string.ts`: the same
 * `code`-first matching (layout-independent -- `Mod+[` is the same physical
 * key on a French keyboard, where `event.key` would be something else
 * entirely) and the same `Mod` token meaning "Cmd on mac, Ctrl elsewhere",
 * minus Paseo's chord support, which smind has no binding for.
 *
 * `event.key` is kept as a *fallback* rather than dropped, because jsdom
 * `fireEvent.keyDown` calls -- and a few real IMEs -- deliver a `key` with
 * no `code`.
 */

/** Modifier + key requirements a KeyboardEvent must satisfy to fire a binding. */
export interface KeyCombo {
  /** `KeyboardEvent.code` this combo wants, or the literal `"Digit"` wildcard (any of Digit0-Digit9). */
  code: string;
  /** Lowercased `KeyboardEvent.key` accepted when the event carries no usable `code`. */
  key?: string;
  /** The shifted character the same physical key produces (`?` for Slash), also accepted via `key`. */
  shiftedKey?: string;
  meta?: true;
  ctrl?: true;
  alt?: true;
  shift?: true;
  /** Cmd on mac, Ctrl everywhere else -- resolved at match time, not parse time. */
  mod?: true;
}

interface KeyMapping {
  code: string;
  key?: string;
  shiftedKey?: string;
}

const KEY_MAP: Record<string, KeyMapping> = {};

for (let i = 0; i < 26; i++) {
  const letter = String.fromCharCode(65 + i);
  KEY_MAP[letter] = { code: `Key${letter}`, key: letter.toLowerCase() };
}

const SHIFTED_DIGITS = [")", "!", "@", "#", "$", "%", "^", "&", "*", "("];
for (let i = 0; i <= 9; i++) {
  KEY_MAP[String(i)] = { code: `Digit${i}`, key: String(i), shiftedKey: SHIFTED_DIGITS[i]! };
}

/** The wildcard standing for any of Digit0-Digit9; the matched digit is handed to the action as its payload. */
export const DIGIT_WILDCARD = "Digit";

KEY_MAP[DIGIT_WILDCARD] = { code: DIGIT_WILDCARD };
KEY_MAP["["] = { code: "BracketLeft", key: "[", shiftedKey: "{" };
KEY_MAP["]"] = { code: "BracketRight", key: "]", shiftedKey: "}" };
KEY_MAP["\\"] = { code: "Backslash", key: "\\", shiftedKey: "|" };
KEY_MAP[","] = { code: "Comma", key: ",", shiftedKey: "<" };
KEY_MAP["."] = { code: "Period", key: ".", shiftedKey: ">" };
KEY_MAP["/"] = { code: "Slash", key: "/", shiftedKey: "?" };
// `?` is its own entry rather than an alias of `/`: a binding written
// `Shift+?` should match the event the user's fingers actually produce,
// whose `key` is already "?" -- see `formatCombo`, which drops the now
// redundant Shift from the display.
KEY_MAP["?"] = { code: "Slash", key: "?" };
KEY_MAP["Space"] = { code: "Space", key: " " };
KEY_MAP["Enter"] = { code: "Enter", key: "enter" };
KEY_MAP["Backspace"] = { code: "Backspace", key: "backspace" };
KEY_MAP["Escape"] = { code: "Escape", key: "escape" };
KEY_MAP["Tab"] = { code: "Tab", key: "tab" };
KEY_MAP["ArrowLeft"] = { code: "ArrowLeft", key: "arrowleft" };
KEY_MAP["ArrowRight"] = { code: "ArrowRight", key: "arrowright" };
KEY_MAP["ArrowUp"] = { code: "ArrowUp", key: "arrowup" };
KEY_MAP["ArrowDown"] = { code: "ArrowDown", key: "arrowdown" };

/** Every key name a combo string may carry -- exported so a rebinding UI can validate without a second hand-kept list. */
export const SHORTCUT_KEY_NAMES: readonly string[] = Object.keys(KEY_MAP);

const CODE_TO_NAME: Record<string, string> = {};
for (const [name, mapping] of Object.entries(KEY_MAP)) {
  if (!CODE_TO_NAME[mapping.code]) CODE_TO_NAME[mapping.code] = name;
}

/** Parses `"Mod+Alt+T"` into a KeyCombo. Throws on an unknown key name or a malformed string, so a bad binding fails loudly at module load rather than silently never firing. */
export function parseCombo(input: string): KeyCombo {
  const parts = input.split("+");
  if (parts.length === 0 || parts.some((p) => p === "")) {
    throw new Error(`invalid shortcut string: "${input}"`);
  }

  const combo: KeyCombo = { code: "" };
  let keyPart: string | null = null;

  for (const part of parts) {
    switch (part) {
      case "Mod":
        combo.mod = true;
        break;
      case "Cmd":
        combo.meta = true;
        break;
      case "Ctrl":
        combo.ctrl = true;
        break;
      case "Alt":
        combo.alt = true;
        break;
      case "Shift":
        combo.shift = true;
        break;
      default:
        if (keyPart !== null) throw new Error(`invalid shortcut string: "${input}" -- two key parts`);
        keyPart = part;
    }
  }

  if (keyPart === null) throw new Error(`invalid shortcut string: "${input}" -- no key part`);
  const mapping = KEY_MAP[keyPart];
  if (!mapping) throw new Error(`unknown key in shortcut string: "${keyPart}"`);

  combo.code = mapping.code;
  if (mapping.key !== undefined) combo.key = mapping.key;
  if (mapping.shiftedKey !== undefined) combo.shiftedKey = mapping.shiftedKey;
  return combo;
}

/** The subset of KeyboardEvent matching needs -- lets the matcher be unit-tested with plain objects. */
export interface KeyEventLike {
  key: string;
  code?: string;
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  repeat?: boolean;
}

/** What a matched combo yields beyond "it matched": the digit behind a `Digit` wildcard. */
export interface ComboMatch {
  digit?: number;
}

function keyMatches(combo: KeyCombo, event: KeyEventLike): ComboMatch | null {
  if (combo.code === DIGIT_WILDCARD) {
    if (event.code?.startsWith("Digit")) return { digit: Number(event.code.slice("Digit".length)) };
    if (/^[0-9]$/.test(event.key)) return { digit: Number(event.key) };
    return null;
  }
  if (event.code) {
    // A usable `code` is authoritative: it survives non-US layouts and the
    // Alt-composed characters (Alt+T is "†" on mac) that make `key`
    // unreliable exactly when a modifier is held.
    return event.code === combo.code ? {} : null;
  }
  const key = event.key.toLowerCase();
  if (combo.key !== undefined && key === combo.key) return {};
  if (combo.shiftedKey !== undefined && event.key === combo.shiftedKey) return {};
  return null;
}

/**
 * Whether event satisfies combo on this platform, returning the match's
 * payload (`{}` for most combos, `{digit}` for the `Digit` wildcard) or
 * null. Modifiers are matched *exactly* -- `Mod+K` does not fire for
 * `Mod+Shift+K`, which is what makes the plan's "not for a near-miss (wrong
 * modifier)" scenario hold.
 */
export function matchCombo(combo: KeyCombo, event: KeyEventLike, isMac: boolean): ComboMatch | null {
  const wantMeta = combo.meta === true || (combo.mod === true && isMac);
  const wantCtrl = combo.ctrl === true || (combo.mod === true && !isMac);
  if (event.metaKey !== wantMeta) return null;
  if (event.ctrlKey !== wantCtrl) return null;
  if (event.altKey !== (combo.alt === true)) return null;
  if (event.shiftKey !== (combo.shift === true)) return null;
  return keyMatches(combo, event);
}

/**
 * A combo's platform-resolved spelling, for comparing two combos rather
 * than displaying one.
 *
 * `Mod+K` and `Ctrl+K` are the same physical shortcut on Linux but
 * different strings, and `Shift+?` and `Shift+/` are the same key -- so
 * string equality is the wrong test for "do these two bindings collide".
 * Canonicalizing to resolved modifiers in a fixed order plus the
 * `KeyboardEvent.code` makes equality mean what a conflict check needs it
 * to mean.
 */
export function canonicalCombo(input: string, isMac: boolean): string {
  const combo = parseCombo(input);
  const parts: string[] = [];
  if (combo.ctrl === true || (combo.mod === true && !isMac)) parts.push("Ctrl");
  if (combo.alt === true) parts.push("Alt");
  if (combo.shift === true) parts.push("Shift");
  if (combo.meta === true || (combo.mod === true && isMac)) parts.push("Cmd");
  parts.push(combo.code);
  return parts.join("+");
}

const MAC_SYMBOLS: Record<string, string> = { Mod: "⌘", Cmd: "⌘", Ctrl: "⌃", Alt: "⌥", Shift: "⇧" };

const KEY_LABELS: Record<string, string> = {
  Escape: "Esc",
  ArrowLeft: "←",
  ArrowRight: "→",
  ArrowUp: "↑",
  ArrowDown: "↓",
  Enter: "↵",
  Backspace: "⌫",
  [DIGIT_WILDCARD]: "1–9",
};

/**
 * The human-readable form of a combo string: `⌘⌥T` on mac, `Ctrl+Alt+T`
 * elsewhere. Formatting the *string* (not the parsed combo) keeps the
 * display faithful to how the binding was written, including the `?` whose
 * Shift is implied by the character itself and so isn't shown twice.
 */
export function formatCombo(input: string, isMac: boolean): string {
  const parts = input.split("+");
  const keyPart = parts[parts.length - 1]!;
  const modifiers = parts.slice(0, -1).filter((p) => !(p === "Shift" && keyPart === "?"));
  const key = KEY_LABELS[keyPart] ?? (keyPart.length === 1 ? keyPart.toUpperCase() : keyPart);

  if (isMac) return [...modifiers.map((m) => MAC_SYMBOLS[m] ?? m), key].join("");
  return [...modifiers.map((m) => (m === "Mod" ? "Ctrl" : m)), key].join("+");
}

/**
 * A chord: one or more combos pressed in sequence, space-separated in a
 * binding's combo string (`"Mod+K S"`). A plain single-combo binding is a
 * chord of length 1 -- there is no separate representation to keep in sync.
 */
export function parseChord(input: string): KeyCombo[] {
  return input.split(" ").map(parseCombo);
}

/** The inverse of {@link parseCombo}: a combo's canonical storage spelling. */
function comboToString(combo: KeyCombo): string {
  const parts: string[] = [];
  if (combo.mod === true) parts.push("Mod");
  if (combo.ctrl === true) parts.push("Ctrl");
  if (combo.alt === true) parts.push("Alt");
  if (combo.shift === true) parts.push("Shift");
  if (combo.meta === true) parts.push("Cmd");
  const name = CODE_TO_NAME[combo.code];
  if (name !== undefined) parts.push(name);
  return parts.join("+");
}

/** The inverse of {@link parseChord}: a parsed chord's storage spelling, for the round trip a rebinding capture writes back. */
export function chordToString(chord: readonly KeyCombo[]): string {
  return chord.map(comboToString).join(" ");
}

/** {@link canonicalCombo}, extended step-by-step over a chord -- what a conflict check compares. */
export function canonicalChord(input: string, isMac: boolean): string {
  return input
    .split(" ")
    .map((step) => canonicalCombo(step, isMac))
    .join(" ");
}

/** {@link formatCombo}, extended step-by-step over a chord for display -- `"⌘K then S"`. */
export function formatChord(input: string, isMac: boolean): string {
  return input
    .split(" ")
    .map((step) => formatCombo(step, isMac))
    .join(" then ");
}

const MODIFIER_CODES = new Set([
  "MetaLeft",
  "MetaRight",
  "ControlLeft",
  "ControlRight",
  "AltLeft",
  "AltRight",
  "ShiftLeft",
  "ShiftRight",
]);

/**
 * Whether a keydown carries a modifier key itself rather than a key pressed
 * with one. The browser emits one of these before every combo that holds a
 * modifier -- mid-chord, that keydown matches no combo, and resolving it
 * would wrongly drop the chord back to its first step. It decides nothing.
 */
export function isModifierKeyCode(code: string | undefined): boolean {
  return code !== undefined && MODIFIER_CODES.has(code);
}

/** The combo string an event spells, or null for a bare modifier press -- the capture side of a rebinding UI. */
export function comboStringFromEvent(event: KeyEventLike): string | null {
  const parts: string[] = [];
  if (event.ctrlKey) parts.push("Ctrl");
  if (event.altKey) parts.push("Alt");
  if (event.shiftKey) parts.push("Shift");
  if (event.metaKey) parts.push("Cmd");

  const name = event.code ? CODE_TO_NAME[event.code] : undefined;
  if (name !== undefined) {
    parts.push(name);
    return parts.join("+");
  }
  if (event.key.length === 1) {
    parts.push(event.key.toUpperCase());
    return KEY_MAP[event.key.toUpperCase()] ? parts.join("+") : null;
  }
  if (KEY_MAP[event.key]) {
    parts.push(event.key);
    return parts.join("+");
  }
  return null;
}

/** True when this session's platform uses Cmd as its primary modifier -- read once per dispatch, never cached across a remount (tests reassign `navigator.platform`). */
export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return false;
  const source = navigator.platform || navigator.userAgent || "";
  return /mac|iphone|ipad|ipod/i.test(source);
}
