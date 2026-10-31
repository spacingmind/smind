/** Window event asking the sidebar to open its create-workspace dialog (the home landing's "Add project"). */
export const NEW_WORKSPACE_EVENT = "smind:new-workspace";

export function requestNewWorkspace(): void {
  window.dispatchEvent(new Event(NEW_WORKSPACE_EVENT));
}
