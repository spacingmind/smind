// themeColors.ts: the strict value grammar for a plugin theme and its conversion to a colour the mobile theme can
// use. The grammar is the daemon's (internal/plugins/theme.go) and web's (isSafeThemeValue): hex, rgb()/rgba(),
// hsl()/hsla(), oklch(), a plain length or `transparent`, at most 64 ASCII bytes, nothing else (no url(), var(),
// calc(), quotes, backslashes, semicolons, braces). Values are parsed, never evaluated or passed on as text.

const NUM = String.raw`[+-]?[0-9]*\.?[0-9]+(?:%|deg)?`;
const SEP = String.raw`(?:[ \t]*,[ \t]*|[ \t]+)`;
const THEME_VALUE_RE = new RegExp(
  '^(?:' +
    '#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})' +
    `|(?:rgb|rgba|hsl|hsla|oklch)\\([ \\t]*${NUM}${SEP}${NUM}${SEP}${NUM}(?:[ \\t]*[,/][ \\t]*[+-]?[0-9]*\\.?[0-9]+%?)?[ \\t]*\\)` +
    String.raw`|-?[0-9]*\.?[0-9]+(?:px|rem|em|%)?` +
    '|transparent' +
    ')$',
);

export function isSafeThemeValue(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 64 && THEME_VALUE_RE.test(value);
}

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const byte = (x: number) => Math.round(clamp01(x) * 255);
const hex2 = (n: number) => n.toString(16).padStart(2, '0');

/** `#rrggbb` when opaque (the form withAlpha accepts), else `rgba(r, g, b, a)`. */
function emit(r: number, g: number, b: number, a: number): string {
  const alpha = clamp01(a);
  if (alpha >= 1) return `#${hex2(byte(r))}${hex2(byte(g))}${hex2(byte(b))}`;
  return `rgba(${byte(r)}, ${byte(g)}, ${byte(b)}, ${Math.round(alpha * 1000) / 1000})`;
}

function pct(token: string, whole: number): number {
  return token.endsWith('%') ? (parseFloat(token) / 100) * whole : parseFloat(token);
}

function hue(token: string): number {
  return ((parseFloat(token) % 360) + 360) % 360;
}

function alphaOf(token: string | undefined): number {
  if (token === undefined) return 1;
  return token.endsWith('%') ? parseFloat(token) / 100 : parseFloat(token);
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const k = (n: number) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return [f(0), f(8), f(4)];
}

function oklchToRgb(L: number, C: number, hDeg: number): [number, number, number] {
  const h = (hDeg * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s];
  const enc = (x: number) => (x <= 0.0031308 ? 12.92 * x : 1.055 * Math.max(0, x) ** (1 / 2.4) - 0.055);
  return [enc(lin[0]), enc(lin[1]), enc(lin[2])];
}

/**
 * The mobile colour for a theme value, or null when the value is unsafe or is not a colour (a plain length). Opaque
 * results are `#rrggbb`; translucent ones `rgba(...)`. oklch is converted here (OKLab -> linear sRGB -> sRGB, clipped
 * to the gamut), so a theme written once for web works on mobile.
 */
export function toMobileColor(value: unknown): string | null {
  if (!isSafeThemeValue(value)) return null;
  const v = value.trim();
  if (v === 'transparent') return 'rgba(0, 0, 0, 0)';
  if (v.startsWith('#')) {
    const h = v.slice(1);
    const full = h.length <= 4 ? h.split('').map((c) => c + c).join('') : h;
    const n = (i: number) => parseInt(full.slice(i, i + 2), 16) / 255;
    return emit(n(0), n(2), n(4), full.length === 8 ? n(6) : 1);
  }
  const open = v.indexOf('(');
  if (open < 0) return null; // a plain length is no colour
  const fn = v.slice(0, open);
  const t = v.slice(open + 1, -1).trim().split(/[ \t]*[,/][ \t]*|[ \t]+/);
  if (t.length !== 3 && t.length !== 4) return null;
  const alpha = alphaOf(t[3]);
  if (fn === 'rgb' || fn === 'rgba') return emit(pct(t[0], 255) / 255, pct(t[1], 255) / 255, pct(t[2], 255) / 255, alpha);
  if (fn === 'hsl' || fn === 'hsla') {
    const [r, g, b] = hslToRgb(hue(t[0]), clamp01(pct(t[1], 1)), clamp01(pct(t[2], 1)));
    return emit(r, g, b, alpha);
  }
  if (fn === 'oklch') {
    const [r, g, b] = oklchToRgb(pct(t[0], 1), pct(t[1], 0.4), hue(t[2]));
    return emit(r, g, b, alpha);
  }
  return null;
}
