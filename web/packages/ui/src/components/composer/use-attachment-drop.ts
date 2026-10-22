import { useCallback, useRef, useState, type DragEvent } from "react";

function hasFiles(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes("Files");
}

/**
 * Drag-and-drop of files onto a pane (ADR-0038 §7). Spread `handlers` on the pane's root: `dragging` is true from
 * the first dragenter carrying files until dragleave/drop, and a drop hands the files to `onFiles`. A depth counter
 * absorbs the enter/leave pairs of nested children, so the overlay does not flicker while the pointer crosses them.
 */
export function useAttachmentDrop({ enabled, onFiles }: { enabled: boolean; onFiles: (files: File[]) => void }) {
  const [dragging, setDragging] = useState(false);
  const depth = useRef(0);

  const onDragEnter = useCallback(
    (e: DragEvent) => {
      if (!enabled || !hasFiles(e)) return;
      e.preventDefault();
      depth.current += 1;
      setDragging(true);
    },
    [enabled],
  );
  const onDragOver = useCallback(
    (e: DragEvent) => {
      if (!enabled || !hasFiles(e)) return;
      // Required for the pane to be a valid drop target.
      e.preventDefault();
      e.dataTransfer.dropEffect = "copy";
    },
    [enabled],
  );
  const onDragLeave = useCallback(
    (e: DragEvent) => {
      if (!enabled || !hasFiles(e)) return;
      depth.current = Math.max(0, depth.current - 1);
      if (depth.current === 0) setDragging(false);
    },
    [enabled],
  );
  const onDrop = useCallback(
    (e: DragEvent) => {
      if (!enabled || !hasFiles(e)) return;
      e.preventDefault();
      depth.current = 0;
      setDragging(false);
      onFiles(Array.from(e.dataTransfer.files));
    },
    [enabled, onFiles],
  );

  return { dragging: enabled && dragging, handlers: { onDragEnter, onDragOver, onDragLeave, onDrop } };
}
