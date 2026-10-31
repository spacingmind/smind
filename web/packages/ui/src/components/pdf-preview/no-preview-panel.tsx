import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Icons } from "@/lib/icons";

/** A calm, non-error panel for a file the app cannot preview: said plainly, no stack of messages. */
export function NoPreviewPanel({
  title,
  description,
  testId = "no-preview-panel",
  action,
  extra,
}: {
  title: string;
  description?: ReactNode;
  testId?: string;
  action?: { label: string; onClick: () => void };
  /** More actions below the description (Open in app, Download, Reveal, Copy path). */
  extra?: ReactNode;
}) {
  return (
    <EmptyState
      testId={testId}
      icon={<Icons.file className="size-6" />}
      title={title}
      description={description}
      action={
        (action || extra) && (
          <div className="flex flex-col items-center gap-2">
            {action && (
              <Button type="button" variant="outline" size="sm" onClick={action.onClick}>
                {action.label}
              </Button>
            )}
            {extra}
          </div>
        )
      }
    />
  );
}

/** What a sniffed kind is called in a sentence. */
export function kindLabel(kind: string): string {
  switch (kind) {
    case "pdf":
      return "a PDF";
    case "text":
      return "a text file";
    case "image":
      return "an image";
    case "office":
      return "an Office document";
    default:
      return "a binary file";
  }
}
