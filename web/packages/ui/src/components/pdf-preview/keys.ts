import type { HostMessage } from "./host-protocol";

/** Zoom steps per key press, and the in/out factors. */
export const ZOOM_STEP = 1.25;

/** The viewer command a keyboard event stands for (Mod+= / Mod+- / Mod+0 zoom), or null. Shared by the app pane and the viewer page. */
export function zoomKeyCommand(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey?: boolean }): HostMessage | null {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return null;
  if (e.key === "=" || e.key === "+") return { type: "zoom", mode: "step", value: ZOOM_STEP };
  if (e.key === "-" || e.key === "_") return { type: "zoom", mode: "step", value: 1 / ZOOM_STEP };
  if (e.key === "0") return { type: "zoom", mode: "fit" };
  return null;
}

/** PageUp / PageDown / Home / End while the pane (not the viewer page) has focus. */
export function pagingKeyCommand(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey?: boolean }): HostMessage | null {
  if (e.ctrlKey || e.metaKey || e.altKey) return null;
  switch (e.key) {
    case "PageDown":
      return { type: "goto", page: "next" };
    case "PageUp":
      return { type: "goto", page: "prev" };
    case "Home":
      return { type: "goto", page: "first" };
    case "End":
      return { type: "goto", page: "last" };
    default:
      return null;
  }
}
