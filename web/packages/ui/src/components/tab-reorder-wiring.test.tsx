// DT-UI D2: pins on how App.tsx wires the strip reorder (the geometry and the layout operation have their own behaviour tests).
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const app = readFileSync("src/App.tsx", "utf8");

describe("tab reorder wiring (source pins on App.tsx)", () => {
  it("a drag over the tab's own strip sets the reorder target and turns the pane drop preview off", () => {
    const over = app.split("const handleDragOver = useCallback(")[1]!.split("const handleDragEnd")[0]!;
    expect(over).toContain("reorderHit(dragged.sourcePaneId");
    const hit = over.split("if (hit) {")[1]!.split("return;")[0]!;
    expect(hit).toContain("setReorderTarget(hit)");
    expect(hit).toContain("setDragOverPaneId(null)");
    expect(hit).toContain("setDropPosition(null)");
    // Off the strip the target is cleared before the existing pane logic runs.
    expect(over).toContain("setReorderTarget(null);");
  });

  it("dropping on a reorder target calls reorderTab with the slot, before the move/split logic", () => {
    const end = app.split("const handleDragEnd = useCallback(")[1]!.split("[dragOverPaneId")[0]!;
    const reorder = end.indexOf("reorderTab(selectedTask.ID, data.tabKey, reorder.paneId, reorder.slot)");
    const move = end.indexOf("moveTab(selectedTask.ID, data.tabKey, targetPaneId)");
    expect(reorder).toBeGreaterThan(-1);
    expect(move).toBeGreaterThan(reorder);
  });

  it("cancelling a drag clears the reorder target, and the indicator is a 4px bar", () => {
    expect(app.split("const handleDragCancel = useCallback(")[1]!.split("}, [])")[0]).toContain("setReorderTarget(null)");
    expect(app).toMatch(/\{reorderTarget && \(\s*<div[^>]*data-testid="tab-drop-indicator"/);
    expect(app).toContain("pointer-events-none fixed z-50 w-1 rounded-full bg-primary");
  });

  it("only the dragged tab's own pane strip counts as a reorder", () => {
    const hit = app.split("function reorderHit(")[1]!.split("\n}\n")[0]!;
    expect(hit).toContain('[data-pane-id="${paneId.replace(');
    expect(hit).toContain("reorderSlotAtX(spans, x)");
  });

  it("the chip uses the drag classes from the draggable's isDragging, and its hover controls obey the drag-active flag", () => {
    expect(app).toContain("const { listeners, setNodeRef, isDragging } = useDraggable({");
    expect(app).toContain("const drag = tabDragClasses({ isDragging, dragActive });");
    expect(app).toContain('className={cn("group/tab max-w-48 gap-1.5", drag.chip)}');
    expect(app).toContain("drag.affordances ?? TAB_AFFORDANCE_REVEAL");
    expect(app).toContain("dragActive={isDragActive}");
  });

  it("a drag sets the grabbing-cursor flag on the root element and index.css reads it", () => {
    expect(app).toContain('document.documentElement.dataset.tabDragging = "true"');
    expect(app).toContain("delete document.documentElement.dataset.tabDragging");
    const css = readFileSync("src/index.css", "utf8");
    expect(css).toContain('html[data-tab-dragging="true"] * {\n  cursor: grabbing !important;');
  });
});

