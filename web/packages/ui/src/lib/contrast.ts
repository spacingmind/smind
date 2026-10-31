/**
 * WCAG contrast maths for design tokens: parses the colour syntaxes that
 * `index.css` token values use and measures them. Pure, no DOM.
 */
export interface Rgba {
  /** sRGB channels, 0..255 */
  r: number;
  g: number;
  b: number;
  /** 0..1 */
  a: number;
}

export type TokenMap = ReadonlyMap<string, string>;

const squash = (v: string) => v.replace(/\s+/g, " ").trim();

function declarationsIn(body: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const d of body.matchAll(/^\s*(--[\w-]+):\s*([^;]+);/gm)) {
    out.set(d[1], squash(d[2]));
  }
  return out;
}

/** Merge every top-level `selector { ... }` block's `--var: value;` pairs. */
function declarations(css: string, selector: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const block of css.matchAll(new RegExp(`^${selector} \\{\\n([\\s\\S]*?)\\n\\}`, "gm"))) {
    for (const [k, v] of declarationsIn(block[1])) out.set(k, v);
  }
  return out;
}

/** The `:root` (light) and `.dark` token maps of an `index.css` source. */
export function parseThemeMaps(css: string): { light: Map<string, string>; dark: Map<string, string> } {
  const light = declarations(css, ":root");
  // `.dark` only overrides: fall back to the light value for the rest.
  const dark = new Map(light);
  for (const [k, v] of declarations(css, "\\.dark")) dark.set(k, v);
  return { light, dark };
}

const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

function linearToSrgb255(c: number): number {
  const v = clamp01(c);
  return 255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);
}

function srgb255ToLinear(c: number): number {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}

function oklchToRgba(L: number, C: number, hDeg: number, a: number): Rgba {
  const h = (hDeg * Math.PI) / 180;
  const A = C * Math.cos(h);
  const B = C * Math.sin(h);
  const l = (L + 0.3963377774 * A + 0.2158037573 * B) ** 3;
  const m = (L - 0.1055613458 * A - 0.0638541728 * B) ** 3;
  const s = (L - 0.0894841775 * A - 1.291485548 * B) ** 3;
  return {
    r: linearToSrgb255(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    g: linearToSrgb255(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    b: linearToSrgb255(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
    a,
  };
}

function parseAlpha(s: string | undefined): number {
  if (s === undefined) return 1;
  return s.endsWith("%") ? parseFloat(s) / 100 : parseFloat(s);
}

function splitTop(s: string, sep: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let cur = "";
  for (const ch of s) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === sep && depth === 0) {
      parts.push(cur.trim());
      cur = "";
    } else cur += ch;
  }
  parts.push(cur.trim());
  return parts;
}

/** Parse a token value to RGBA, resolving `var(--x)` through `vars`. */
export function parseColor(value: string, vars: TokenMap = new Map()): Rgba {
  const v = squash(value);
  const ref = /^var\((--[\w-]+)\)$/.exec(v);
  if (ref) {
    const next = vars.get(ref[1]);
    if (next === undefined) throw new Error(`unresolved ${ref[1]}`);
    return parseColor(next, vars);
  }
  if (v === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(v);
  if (hex) {
    const h = hex[1].length === 3 ? [...hex[1]].map((c) => c + c).join("") : hex[1];
    return { r: parseInt(h.slice(0, 2), 16), g: parseInt(h.slice(2, 4), 16), b: parseInt(h.slice(4, 6), 16), a: 1 };
  }
  const rgb = /^rgba?\(([^)]*)\)$/.exec(v);
  if (rgb) {
    const [r, g, b, a] = rgb[1].split(/[\s,/]+/).filter(Boolean);
    return { r: parseFloat(r), g: parseFloat(g), b: parseFloat(b), a: parseAlpha(a) };
  }
  const ok = /^oklch\(([^)]*)\)$/.exec(v);
  if (ok) {
    const [body, alpha] = ok[1].split("/").map((s) => s.trim());
    const [L, C, H] = body.split(/\s+/);
    const l = L.endsWith("%") ? parseFloat(L) / 100 : parseFloat(L);
    return oklchToRgba(l, parseFloat(C), parseFloat(H), parseAlpha(alpha));
  }
  const mix = /^color-mix\((.*)\)$/.exec(v);
  if (mix) {
    const [space, first, second] = splitTop(mix[1], ",");
    if (!/^in (oklab|oklch|srgb)$/.test(space) || second !== "transparent") {
      throw new Error(`unsupported color-mix: ${v}`);
    }
    const m = /^(.*) (\d+(?:\.\d+)?)%$/.exec(first);
    if (!m) throw new Error(`unsupported color-mix: ${v}`);
    const c = parseColor(m[1], vars);
    // Mixing with transparent keeps the hue and scales the alpha.
    return { ...c, a: c.a * (parseFloat(m[2]) / 100) };
  }
  throw new Error(`unsupported colour: ${v}`);
}

/** Alpha-composite `fg` over an opaque `surface`. */
export function composite(fg: Rgba, surface: Rgba): Rgba {
  const a = fg.a;
  return {
    r: fg.r * a + surface.r * (1 - a),
    g: fg.g * a + surface.g * (1 - a),
    b: fg.b * a + surface.b * (1 - a),
    a: 1,
  };
}

/** WCAG relative luminance of an opaque colour. */
export function luminance(c: Rgba): number {
  return 0.2126 * srgb255ToLinear(c.r) + 0.7152 * srgb255ToLinear(c.g) + 0.0722 * srgb255ToLinear(c.b);
}

/** WCAG contrast ratio of an opaque foreground on an opaque background. */
export function contrastRatio(fg: Rgba, bg: Rgba): number {
  const a = luminance(fg);
  const b = luminance(bg);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

export function toHex(c: Rgba): string {
  const h = (n: number) => Math.round(Math.min(255, Math.max(0, n))).toString(16).padStart(2, "0");
  return `#${h(c.r)}${h(c.g)}${h(c.b)}`;
}

/**
 * Contrast of token `fg` on token `surface`. A translucent `surface` (e.g.
 * `--hover`) is composited over the opaque `base` token first; a translucent
 * `fg` is composited over the resulting surface.
 */
export function tokenContrast(vars: TokenMap, fg: string, surface: string, base?: string): number {
  let bg = parseColor(`var(${surface})`, vars);
  if (bg.a < 1) {
    if (!base) throw new Error(`${surface} is translucent: pass a base token`);
    bg = composite(bg, parseColor(`var(${base})`, vars));
  }
  return contrastRatio(composite(parseColor(`var(${fg})`, vars), bg), bg);
}
