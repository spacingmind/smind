import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { DEFAULT_CHUNK_BYTES, DEFAULT_MAX_READ_BYTES, FileChangedError, FileTooLargeError, readFileBytes } from "@/lib/file-bytes";
import type { DaemonFilesInfo, FileStatResult, Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";
import { acceptViewerEvent, type HostMessage } from "./host-protocol";
import { pagingKeyCommand, zoomKeyCommand } from "./keys";
import { PDF_LIMITS } from "./limits";
import { NoPreviewPanel } from "./no-preview-panel";
import { PdfViewerFrame } from "./pdf-viewer-frame";

type Failure = "oversize" | "password" | "corrupt" | "truncated" | "timeout" | "conflict" | "error";
type Phase = "loading" | "ready" | Failure;

/** True when the last KiB of the file holds the PDF end marker: a file without it was cut off. */
export function hasEofMarker(bytes: Uint8Array): boolean {
  const tail = bytes.subarray(Math.max(0, bytes.length - 1024));
  const marker = [0x25, 0x25, 0x45, 0x4f, 0x46]; // %%EOF
  for (let i = 0; i + marker.length <= tail.length; i++) {
    if (marker.every((b, j) => tail[i + j] === b)) return true;
  }
  return false;
}

function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(n >= 10 * 1024 * 1024 ? 0 : 1)} MiB`;
  if (n >= 1024) return `${Math.round(n / 1024)} KiB`;
  return `${n} bytes`;
}

/**
 * The PDF half of a file tab (ADR-0049 section 3): reads the file through `file.stat` / `file.readRange` (chunks capped by the transport's
 * `daemon.info.files`), hands the bytes to the sandboxed viewer iframe by postMessage, and shows one calm panel per failure. The
 * viewer never receives a path, task id or token; the host accepts only the four typed messages from its own frame.
 */
export function PdfPreviewPane({
  client,
  task,
  path,
  stat: initialStat,
}: {
  client: WsClientLike | null;
  task: Task;
  path: string;
  /** The `file.stat` that routed here: a sniffed, non-mismatched PDF. */
  stat: FileStatResult;
}) {
  const [stat, setStat] = useState(initialStat);
  const [phase, setPhase] = useState<Phase>("loading");
  const [detail, setDetail] = useState("");
  const [limit, setLimit] = useState(DEFAULT_MAX_READ_BYTES);
  const [mounted, setMounted] = useState(false); // the viewer iframe exists
  const [pages, setPages] = useState<{ current: number; count: number; zoom: number } | null>(null);
  const [nonce, setNonce] = useState(0);

  const frameRef = useRef<HTMLIFrameElement>(null);
  const bytesRef = useRef<Uint8Array | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const phaseRef = useRef<Phase>("loading");
  phaseRef.current = phase;

  const clearTimer = useCallback(() => {
    if (timer.current !== undefined) window.clearTimeout(timer.current);
    timer.current = undefined;
  }, []);

  const fail = useCallback(
    (reason: Failure, text = "") => {
      clearTimer();
      bytesRef.current = null;
      setMounted(false); // tears the iframe (and its worker) down
      setPages(null);
      setDetail(text);
      setPhase(reason);
    },
    [clearTimer],
  );

  // Read the bytes (re-runs on reload and when the file's version changes).
  useEffect(() => {
    if (!client) return;
    const ctl = new AbortController();
    clearTimer();
    bytesRef.current = null;
    setMounted(false);
    setPages(null);
    setPhase("loading");
    (async () => {
      let files: DaemonFilesInfo | undefined;
      try {
        files = (await client.call<{ files?: DaemonFilesInfo }>("daemon.info", undefined, { signal: ctl.signal }))?.files;
      } catch {
        files = undefined; // an older daemon, or the call failed: assume the smallest transport
      }
      const maxBytes = files?.maxReadBytes ?? DEFAULT_MAX_READ_BYTES;
      setLimit(maxBytes);
      if (stat.size > maxBytes) return fail("oversize");
      try {
        const bytes = await readFileBytes(client, {
          taskId: task.ID,
          path,
          size: stat.size,
          mtime: stat.mtime,
          chunkBytes: files?.maxChunkBytes ?? DEFAULT_CHUNK_BYTES,
          maxBytes,
          signal: ctl.signal,
        });
        if (ctl.signal.aborted) return;
        bytesRef.current = bytes;
        setMounted(true);
      } catch (err) {
        if (ctl.signal.aborted) return;
        if (err instanceof FileTooLargeError) return fail("oversize");
        if (err instanceof FileChangedError) return fail("conflict");
        fail("error", err instanceof Error ? err.message : String(err));
      }
    })();
    return () => ctl.abort();
    // task.ID, not task: a fresh Task object with the same ID must not re-read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, task.ID, path, stat.mtime, stat.size, nonce]);

  // The viewer's messages. Only the four typed shapes from our own frame get through.
  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const msg = acceptViewerEvent(event, frameRef.current?.contentWindow);
      if (!msg) return;
      switch (msg.type) {
        case "ready": {
          const bytes = bytesRef.current;
          const win = frameRef.current?.contentWindow;
          if (!bytes || !win || phaseRef.current !== "loading") return;
          const copy = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
          const load: HostMessage = { type: "load", data: copy };
          // "*": an opaque-origin frame has no origin to name. The frame is our own iframe (checked on every inbound message).
          win.postMessage(load, "*", [copy]);
          clearTimer();
          timer.current = window.setTimeout(() => fail("timeout"), PDF_LIMITS.parseTimeoutMs);
          return;
        }
        case "pages":
          clearTimer();
          setPages({ current: 1, count: msg.count, zoom: 1 });
          setPhase("ready");
          return;
        case "page":
          setPages({ current: msg.current, count: msg.count, zoom: msg.zoom });
          return;
        case "error":
          if (msg.reason === "password") return fail("password");
          if (msg.reason === "timeout") return fail("timeout");
          return fail(bytesRef.current && !hasEofMarker(bytesRef.current) ? "truncated" : "corrupt");
      }
    };
    window.addEventListener("message", onMessage);
    return () => {
      window.removeEventListener("message", onMessage);
      clearTimer();
    };
  }, [clearTimer, fail]);

  const send = useCallback((msg: HostMessage) => {
    frameRef.current?.contentWindow?.postMessage(msg, "*");
  }, []);

  // Reload: re-stat; refetch when the file's version changed (or when the last attempt failed).
  const reload = useCallback(async () => {
    if (!client) return;
    try {
      const next = await client.call<FileStatResult>("file.stat", { taskId: task.ID, path });
      if (next.kind !== "pdf" || next.mismatch) return fail("corrupt", "The file is no longer a PDF.");
      if (next.mtime === stat.mtime && next.size === stat.size && phaseRef.current === "ready") return;
      setStat(next);
      setNonce((n) => n + 1);
    } catch (err) {
      fail("error", err instanceof Error ? err.message : String(err));
    }
  }, [client, task.ID, path, stat.mtime, stat.size, fail]);

  function onKeyDown(e: React.KeyboardEvent) {
    const cmd = zoomKeyCommand(e.nativeEvent) ?? pagingKeyCommand(e.nativeEvent);
    if (!cmd || phase !== "ready") return;
    e.preventDefault();
    send(cmd);
  }

  const showPanel = phase !== "loading" && phase !== "ready";
  return (
    <div className="flex h-full min-h-0 flex-col outline-none" data-testid="pdf-preview-pane" data-phase={phase} tabIndex={0} onKeyDown={onKeyDown}>
      <PaneHeader
        testId="file-editor-header"
        title={<span data-testid="file-editor-path">{path}</span>}
        actions={
          <>
            {phase === "ready" && pages && (
              <>
                <span className="text-ui-sm tabular-nums text-foreground-muted" data-testid="pdf-page-indicator" aria-live="polite">
                  {pages.current} / {pages.count}
                </span>
                <Button type="button" variant="outline" size="sm" aria-label="Zoom out" onClick={() => send({ type: "zoom", mode: "step", value: 0.8 })}>
                  −
                </Button>
                <span className="w-10 text-center text-ui-sm tabular-nums text-foreground-muted" data-testid="pdf-zoom">
                  {Math.round(pages.zoom * 100)}%
                </span>
                <Button type="button" variant="outline" size="sm" aria-label="Zoom in" onClick={() => send({ type: "zoom", mode: "step", value: 1.25 })}>
                  +
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={() => send({ type: "zoom", mode: "fit" })}>
                  Fit
                </Button>
              </>
            )}
            <Button type="button" variant="outline" size="sm" onClick={() => void reload()}>
              Reload
            </Button>
          </>
        }
      />
      {phase === "loading" && (
        <div className="flex flex-1 items-center justify-center" data-testid="pdf-loading">
          <InlineSpinner label="Loading…" />
        </div>
      )}
      {showPanel && <FailurePanel phase={phase} detail={detail} size={stat.size} limit={limit} onReload={() => void reload()} />}
      {mounted && (
        <PdfViewerFrame
          ref={frameRef}
          className={phase === "ready" ? "min-h-0 w-full flex-1 border-0 bg-transparent" : "pointer-events-none invisible absolute h-px w-full border-0"}
        />
      )}
    </div>
  );
}

function FailurePanel({
  phase,
  detail,
  size,
  limit,
  onReload,
}: {
  phase: Failure;
  detail: string;
  size: number;
  limit: number;
  onReload: () => void;
}) {
  const reload = { label: "Reload", onClick: onReload };
  const id = `pdf-state-${phase}`;
  switch (phase) {
    case "oversize":
      return (
        <NoPreviewPanel
          testId={id}
          title="This PDF is too large to preview"
          description={`It is ${formatBytes(size)}; previews stop at ${formatBytes(limit)}.`}
        />
      );
    case "password":
      return (
        <NoPreviewPanel testId={id} title="This PDF is password protected" description="Previewing password-protected PDFs is not supported yet." />
      );
    case "truncated":
      return (
        <NoPreviewPanel
          testId={id}
          title="This PDF looks incomplete"
          description="The file ends before the PDF does. It may still be being written."
          action={reload}
        />
      );
    case "timeout":
      return (
        <NoPreviewPanel
          testId={id}
          title="This PDF could not be opened"
          description="It took longer than 30 seconds to open."
          action={reload}
        />
      );
    case "conflict":
      return (
        <NoPreviewPanel testId={id} title="This file changed while it was being read" description="Reload to read the new version." action={reload} />
      );
    case "error":
      return <NoPreviewPanel testId={id} title="This PDF could not be read" description={detail || undefined} action={reload} />;
    default:
      return (
        <NoPreviewPanel
          testId={id}
          title="This PDF could not be opened"
          description={detail || "The file may be damaged."}
          action={reload}
        />
      );
  }
}
