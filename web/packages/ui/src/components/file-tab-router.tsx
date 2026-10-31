import { useEffect, useState } from "react";

import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { NoPreviewPanel, kindLabel } from "@/components/pdf-preview/no-preview-panel";
import { PdfPreviewPane } from "@/components/pdf-preview/pdf-preview-pane";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { FileStatResult, Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

type Route =
  | { kind: "checking" }
  | { kind: "pdf"; stat: FileStatResult }
  | { kind: "other"; stat: FileStatResult }
  | { kind: "mismatch"; stat: FileStatResult };

/**
 * The binary half of a file tab (ADR-0049 section 3.1), loaded lazily by FileEditorPane so that none of it (the stat routing, the panels,
 * the PDF pane) is part of the app's entry chunk. It asks `file.stat` what the file really is, then shows the PDF preview, a mismatch
 * panel or a "no preview" panel. Anything that is plain text, and any stat failure (an older daemon has no `file.stat`), is handed back
 * to the editor through `onText`, which then shows its own honest result. No new TabKind: the tab key is still `${taskId}:file:${path}`.
 */
export default function FileTabRouter({
  client,
  task,
  path,
  onText,
}: {
  client: WsClientLike | null;
  task: Task;
  path: string;
  events?: DaemonEvents | null;
  onText: () => void;
}) {
  const [route, setRoute] = useState<Route>({ kind: "checking" });

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<FileStatResult>("file.stat", { taskId: task.ID, path })
      .then((stat) => {
        if (cancelled) return;
        if (stat.mismatch) setRoute({ kind: "mismatch", stat });
        else if (stat.kind === "pdf" && stat.previewable) setRoute({ kind: "pdf", stat });
        else if (stat.kind === "text") onText();
        else setRoute({ kind: "other", stat });
      })
      .catch(() => {
        if (!cancelled) onText();
      });
    return () => {
      cancelled = true;
    };
    // onText is a stable setter from the parent; re-running on its identity would re-stat the file.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, task.ID, path]);

  const header = <PaneHeader testId="file-editor-header" title={<span data-testid="file-editor-path">{path}</span>} />;
  switch (route.kind) {
    case "checking":
      return (
        <div className="flex h-full min-h-0 flex-col" data-testid="file-editor-pane">
          {header}
          <div className="flex flex-1 items-center justify-center">
            <InlineSpinner label="Loading…" />
          </div>
        </div>
      );
    case "pdf":
      return <PdfPreviewPane client={client} task={task} path={path} stat={route.stat} />;
    case "mismatch":
      return (
        <div className="flex h-full min-h-0 flex-col" data-testid="file-editor-pane">
          {header}
          <NoPreviewPanel
            testId="file-mismatch-panel"
            title="This file is not what its name says"
            description={`It is named ${route.stat.ext || "(no extension)"} but looks like ${kindLabel(route.stat.kind)}, so it is not previewed.`}
            action={route.stat.kind === "text" ? { label: "Open as text", onClick: onText } : undefined}
          />
        </div>
      );
    case "other":
      return (
        <div className="flex h-full min-h-0 flex-col" data-testid="file-editor-pane">
          {header}
          <NoPreviewPanel
            testId="no-preview-panel"
            title="No preview for this file type"
            description={`This is ${kindLabel(route.stat.kind)}. It can not be shown here.`}
          />
        </div>
      );
  }
}
