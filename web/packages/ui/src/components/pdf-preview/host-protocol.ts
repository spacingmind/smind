import { PDF_LIMITS } from "./limits";

/**
 * The postMessage protocol between the app (host) and the sandboxed PDF viewer iframe (ADR-0049 section 3.3).
 *
 * host -> viewer: bytes and view commands only. The viewer never receives a path, a task id or a token.
 * viewer -> host: four typed messages. The host validates each field and the sender; anything else is ignored.
 */

export type PdfError = "password" | "corrupt" | "timeout";

export type ViewerMessage =
  | { type: "ready" }
  | { type: "pages"; count: number; total: number }
  | { type: "page"; current: number; count: number; zoom: number }
  | { type: "error"; reason: PdfError };

export type HostMessage =
  | { type: "load"; data: ArrayBuffer }
  | { type: "zoom"; mode: "fit" | "step" | "set"; value?: number }
  | { type: "goto"; page: number | "first" | "last" | "next" | "prev" };

const ERRORS: readonly string[] = ["password", "corrupt", "timeout"];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function isCount(v: unknown, max: number = PDF_LIMITS.maxPages): v is number {
  return typeof v === "number" && Number.isInteger(v) && v >= 1 && v <= max;
}

/** A viewer message of exactly one of the four shapes, or null. Extra fields are dropped, never trusted. */
export function parseViewerMessage(data: unknown): ViewerMessage | null {
  if (!isObject(data)) return null;
  switch (data.type) {
    case "ready":
      return { type: "ready" };
    case "pages":
      if (!isCount(data.count) || !isCount(data.total, 10_000_000) || data.total < data.count) return null;
      return { type: "pages", count: data.count, total: data.total };
    case "page": {
      if (!isCount(data.count) || !isCount(data.current) || data.current > data.count) return null;
      if (typeof data.zoom !== "number" || !Number.isFinite(data.zoom) || data.zoom <= 0 || data.zoom > 100) return null;
      return { type: "page", current: data.current, count: data.count, zoom: data.zoom };
    }
    case "error":
      if (typeof data.reason !== "string" || !ERRORS.includes(data.reason)) return null;
      return { type: "error", reason: data.reason as PdfError };
    default:
      return null;
  }
}

/** A message event from `frame` carrying a valid viewer message, or null: wrong sender, wrong shape, or anything else. */
export function acceptViewerEvent(event: { source: unknown; data: unknown }, frame: Window | null | undefined): ViewerMessage | null {
  if (!frame || event.source !== frame) return null;
  return parseViewerMessage(event.data);
}

/** The viewer side: a host message of one of three shapes, or null. */
export function parseHostMessage(data: unknown): HostMessage | null {
  if (!isObject(data)) return null;
  switch (data.type) {
    case "load":
      return data.data instanceof ArrayBuffer ? { type: "load", data: data.data } : null;
    case "zoom": {
      if (data.mode !== "fit" && data.mode !== "step" && data.mode !== "set") return null;
      if (data.mode === "fit") return { type: "zoom", mode: "fit" };
      if (typeof data.value !== "number" || !Number.isFinite(data.value)) return null;
      return { type: "zoom", mode: data.mode, value: data.value };
    }
    case "goto": {
      const p = data.page;
      if (p === "first" || p === "last" || p === "next" || p === "prev") return { type: "goto", page: p };
      if (isCount(p)) return { type: "goto", page: p };
      return null;
    }
    default:
      return null;
  }
}
