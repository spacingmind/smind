// A plugin-supplied inline svg icon, rebuilt as React elements from an
// allowlist: parse with DOMParser, then re-emit only known geometry elements
// carrying only known attributes. Untrusted markup never reaches the DOM as
// markup -- no script, no foreignObject, no event handlers, no url() paint
// server except a same-document #id reference, no javascript:/data: values.
import { createElement, type ReactNode } from "react";

import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

const ALLOWED_ELEMENTS = new Set([
  "svg", "g", "path", "circle", "ellipse", "rect", "line", "polyline", "polygon",
  "defs", "linearGradient", "radialGradient", "stop", "clipPath", "title", "desc",
]);

const ALLOWED_ATTRS = new Set([
  "viewBox", "d", "fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin",
  "fill-rule", "clip-rule", "cx", "cy", "r", "rx", "ry", "x", "y", "x1", "y1", "x2", "y2",
  "width", "height", "points", "transform", "opacity", "offset", "stop-color",
  "stop-opacity", "id", "gradientUnits", "gradientTransform",
]);

/** Hyphenated svg attribute names -> React's camelCase prop names. */
const ATTR_PROPS: Record<string, string> = {
  "stroke-width": "strokeWidth",
  "stroke-linecap": "strokeLinecap",
  "stroke-linejoin": "strokeLinejoin",
  "fill-rule": "fillRule",
  "clip-rule": "clipRule",
  "stop-color": "stopColor",
  "stop-opacity": "stopOpacity",
};

/** The only permitted url() reference: one to a same-document id. */
const URL_REF = /^url\(#[A-Za-z0-9._-]+\)$/;

function isSafeAttrValue(value: string): boolean {
  if (value.includes("javascript:") || value.includes("data:")) return false;
  if (value.includes("url(") && !URL_REF.test(value)) return false;
  return true;
}

function propsFrom(el: Element): Record<string, string> {
  const props: Record<string, string> = {};
  for (const attr of Array.from(el.attributes)) {
    if (!ALLOWED_ATTRS.has(attr.name) || !isSafeAttrValue(attr.value)) continue;
    props[ATTR_PROPS[attr.name] ?? attr.name] = attr.value;
  }
  return props;
}

function childrenFrom(el: Element): ReactNode[] {
  const out: ReactNode[] = [];
  el.childNodes.forEach((child) => {
    if (child.nodeType === 3) {
      const text = child.nodeValue ?? "";
      if (text.trim() !== "") out.push(text);
    } else if (child.nodeType === 1) {
      const ce = child as Element;
      if (ALLOWED_ELEMENTS.has(ce.localName ?? ce.nodeName)) {
        out.push(buildElement(ce, { key: String(out.length) }));
      }
    }
  });
  return out;
}

function buildElement(el: Element, extra: { key?: string }): ReactNode {
  const tag = el.localName ?? el.nodeName;
  const isRoot = tag === "svg" && el.parentElement === null;
  const props: Record<string, unknown> = { ...propsFrom(el) };
  if (extra.key !== undefined) props.key = extra.key;
  if (isRoot) {
    props.className = cn("size-4 shrink-0", props.className as string | undefined);
    props["aria-hidden"] = true;
    // Keep the document's own stroke; only default the root to currentColor.
    if (props.stroke === undefined) props.stroke = "currentColor";
  }
  return createElement(tag, props, ...childrenFrom(el));
}

export function SafeSvg({ svg, className }: { svg: string; className?: string }) {
  let root: Element | null = null;
  try {
    const parsed = new DOMParser().parseFromString(svg, "image/svg+xml").documentElement;
    // A malformed document parses to a <parsererror> root instead of throwing.
    if (parsed && (parsed.localName ?? parsed.nodeName) !== "parsererror") root = parsed;
  } catch {
    root = null;
  }
  if (!root || (root.localName ?? root.nodeName) !== "svg") {
    const Cmp = Icons.plugin;
    return <Cmp aria-hidden className={className} />;
  }
  return buildElement(root, { key: undefined });
}
