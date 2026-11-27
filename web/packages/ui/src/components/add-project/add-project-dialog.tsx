import { useEffect, useState } from "react";

import { HostStep } from "@/components/add-project/host-step";
import { MethodCards } from "@/components/add-project/method-cards";
import { OpenFolderStep, type OpenFolderValue } from "@/components/add-project/open-folder-step";
import { useFolderCheck } from "@/hooks/use-folder-check";
import { useProjectHosts } from "@/hooks/use-project-hosts";
import { useWindowState } from "@/hooks/use-window-state";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { daemonSentence } from "@/lib/daemon-sentence";
import { expandHome, normalizeProjectPath, samePath } from "@/lib/project-path";
import { mergeWorkspaces, recordRecent } from "@/lib/project-recents";
import { desktopOS, isDesktop } from "@/lib/platform";
import { cn } from "@/lib/utils";
import { hasTrafficLights } from "@/lib/window-chrome";
import { recentWorkspaceParentDirs } from "@/lib/workspace-tree";
import type { Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** The settings dialog's small-size breakpoints (UI cleanup B3 D2): at or below either one this dialog is the full-bleed sheet. */
const SHEET_MAX_WIDTH = 860;
const SHEET_MAX_HEIGHT = 560;

/** jsdom has no layout engine, so the sheet layout also carries a `data-sheet` attribute a test can assert. */
function useSheetLayout(): boolean {
  const isSheet = () => window.innerWidth <= SHEET_MAX_WIDTH || window.innerHeight <= SHEET_MAX_HEIGHT;
  const [sheet, setSheet] = useState(isSheet);
  useEffect(() => {
    window.addEventListener("resize", isSheet);
    setSheet(isSheet());
    return () => window.removeEventListener("resize", isSheet);
  }, []);
  return sheet;
}

const EMPTY: OpenFolderValue = { path: "", name: "" };

/**
 * The "Add project" modal (C1 D1): stacked steps -- Where (HostStep), How
 * (MethodCards), then the details of the live method (OpenFolderStep). About
 * 640px and centred at desktop sizes; at the settings dialog's small sizes it
 * is the full-bleed sheet, exactly as that dialog does it. Submit creates the
 * workspace with the checked path (expanded, normalised), records it in the
 * host's recents and hands it to `onProjectReady`; a duplicate path opens the
 * existing project instead of creating a second one (D5). A daemon error is a
 * sentence and the dialog stays open with everything typed still in place (D6).
 */
export function AddProjectDialog(props: {
  client: WsClient | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The current workspaces: the recents merge, the duplicate find-or-switch and the picker's quick-jump row read them. */
  workspaces: Workspace[];
  /** Called with the workspace the flow landed on: "created", or "existing" when a duplicate path was opened (D5). */
  onProjectReady: (workspace: Workspace, how: "created" | "existing") => void;
  /** Opens the New task dialog for the workspace, from the later done state (D6). */
  onStartTask: (workspace: Workspace) => void;
}) {
  const { client, open, onOpenChange, workspaces, onProjectReady } = props;
  const [value, setValue] = useState<OpenFolderValue>(EMPTY);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const hosts = useProjectHosts();
  const folder = useFolderCheck(client, workspaces);
  const { fullscreen } = useWindowState();
  // macOS traffic lights float over the window's top-left; in sheet mode the
  // sheet starts below that native title-bar band (copied from SettingsDialog).
  const lights = isDesktop && hasTrafficLights(desktopOS, fullscreen);
  const sheet = useSheetLayout();
  // The check's blocking conclusions (C1 D3): a path a check judged to be
  // missing, not a repo, relative, or empty-after-trim never reaches the
  // daemon. Before anything is judged ("unknown", the initial state, or still
  // "checking") the button stays live: the daemon is the authority, and an
  // empty submit says "Path is required." itself.
  const blocked =
    folder.status.kind === "not-git" ||
    folder.status.kind === "missing" ||
    folder.status.kind === "relative" ||
    folder.status.kind === "empty";
  const duplicate = folder.status.kind === "duplicate";

  // Real recents (C1 D4): the host's current workspaces are merged in on every
  // open, so a project removed later stays in the list until it is dismissed.
  useEffect(() => {
    if (!open) return;
    mergeWorkspaces(hosts.hostKey, workspaces);
  }, [open, hosts.hostKey, workspaces]);

  async function submit(): Promise<void> {
    const trimmed = value.path.trim();
    if (!trimmed) {
      setError("Path is required.");
      return;
    }
    // The path submit sends is the one the check judges: `~` expanded with the
    // daemon's home (the daemon does not expand it), then normalised.
    let home = folder.home;
    if (trimmed.startsWith("~") && home === null) {
      // Submitted with Enter before the path ever lost focus: judge it now, so `~` is still expanded.
      const judged = await folder.check(trimmed);
      if (judged.kind === "relative" || judged.kind === "missing" || judged.kind === "not-git") return;
      home = folder.getHome();
    }
    const expanded = trimmed.startsWith("~") && home !== null ? expandHome(trimmed, home) : trimmed;
    const path = normalizeProjectPath(expanded);
    // A duplicate path opens the existing project; no second workspace on one path (D5).
    const existing = workspaces.find((ws) => samePath(ws.Path, path));
    if (existing) {
      setValue(EMPTY);
      onProjectReady(existing, "existing");
      onOpenChange(false);
      return;
    }
    if (!client) return;
    setPending(true);
    setError(null);
    try {
      const ws = await client.call<Workspace>("workspace.create", {
        path,
        ...(value.name.trim() ? { title: value.name.trim() } : {}),
      });
      recordRecent(hosts.hostKey, { path, title: ws.Title });
      setValue(EMPTY);
      onProjectReady(ws, "created");
      onOpenChange(false);
    } catch (err) {
      // The dialog stays open with the path and the name kept: the person
      // fixes what the daemon refused and tries again (D6).
      setError(daemonSentence(err instanceof Error ? err.message : String(err)));
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <DialogContent
          data-testid="add-project-dialog"
          data-sheet={sheet ? "true" : undefined}
          className={cn(
            "sm:max-w-[640px]",
            // The full-bleed sheet at small sizes, exactly as SettingsDialog
            // does it: drop the centring, size and radius.
            "max-[860px]:inset-0 max-[860px]:left-0 max-[860px]:top-0 max-[860px]:h-auto max-[860px]:w-auto max-[860px]:max-w-none max-[860px]:translate-x-0 max-[860px]:translate-y-0 max-[860px]:rounded-none max-[860px]:border-0",
            "[@media(max-height:560px)]:inset-0 [@media(max-height:560px)]:left-0 [@media(max-height:560px)]:top-0 [@media(max-height:560px)]:h-auto [@media(max-height:560px)]:w-auto [@media(max-height:560px)]:max-w-none [@media(max-height:560px)]:translate-x-0 [@media(max-height:560px)]:translate-y-0 [@media(max-height:560px)]:rounded-none [@media(max-height:560px)]:border-0",
            lights && "max-[860px]:pt-[38px] [@media(max-height:560px)]:pt-[38px]",
          )}
        >
          {lights && (
            <div
              aria-hidden
              data-tauri-drag-region="deep"
              className="absolute inset-x-0 top-0 hidden h-[38px] max-[860px]:block [@media(max-height:560px)]:block"
            />
          )}
          <DialogHeader>
            <DialogTitle>Add project</DialogTitle>
            <DialogDescription>
              Must be an existing git repository -- tasks are created as worktrees branched off it.
            </DialogDescription>
          </DialogHeader>
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <HostStep />
            <MethodCards />
            <OpenFolderStep
              client={client}
              value={value}
              onChange={setValue}
              recentPaths={recentWorkspaceParentDirs(workspaces)}
              status={folder.status}
              onCheck={folder.check}
              hostKey={hosts.hostKey}
              hostName={hosts.current.name}
              workspaces={workspaces}
            />
            <FormError message={error} testId="add-project-error" />
            <DialogFooter>
              <Button type="button" variant="quiet" data-testid="add-project-cancel" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending || blocked} data-testid="add-project-submit">
                {pending ? "Adding…" : duplicate ? "Open project" : "Add project"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
