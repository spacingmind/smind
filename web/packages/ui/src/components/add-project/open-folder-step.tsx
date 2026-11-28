import { useState } from "react";

import { Button } from "@/components/ui/button";
import { FolderPickerDialog } from "@/components/folder-picker-dialog";
import { Input } from "@/components/ui/input";
import type { FolderStatus } from "@/hooks/use-folder-check";
import { Icons } from "@/lib/icons";
import { removeRecent, visibleRecents } from "@/lib/project-recents";
import { cn } from "@/lib/utils";
import type { Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** What the dialog reads out of this step: the typed (or picked) path and the optional title. */
export interface OpenFolderValue {
  path: string;
  name: string;
}

/**
 * The "Add project" dialog's details for the one live method (C1 D3): a path
 * input with the in-app folder chooser ("Browse…"), the folder check's inline
 * status under it, the host's real recents (C1 D4), and the optional Name
 * collapsed behind its own disclosure (D8). Controlled by the dialog: the
 * value and the check live there so submit and the error row see the same
 * state.
 */
export function OpenFolderStep({
  client,
  value,
  onChange,
  recentPaths = [],
  status,
  onCheck,
  hostKey,
  hostName,
  workspaces,
}: {
  client: WsClient | null;
  value: OpenFolderValue;
  onChange: (next: OpenFolderValue) => void;
  /** Parent directories of already-registered workspaces, for the folder picker's quick-jump row. */
  recentPaths?: string[];
  /** The folder check's latest conclusion about the typed path. */
  status: FolderStatus;
  /** Runs the folder check: on blur, and whenever the path is set programmatically. */
  onCheck: (raw: string) => Promise<FolderStatus>;
  /** The recents key (C1 D4): the desktop connection id, or the page origin on the web. */
  hostKey: string;
  /** The host the "does not exist" hint names. */
  hostName: string;
  /** The current workspaces: the recents list hides the projects already in the sidebar. */
  workspaces: Workspace[];
}) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const [nameOpen, setNameOpen] = useState(false);
  // project-recents is module state, not React state: this bump re-renders so a removed row disappears.
  const [, bumpRecents] = useState(0);
  const recents = visibleRecents(hostKey, workspaces).slice(0, 5);

  /** Sets the path from anywhere other than typing (the chooser, a recent) and checks it right away. */
  function setPath(path: string): void {
    onChange({ ...value, path });
    void onCheck(path);
  }

  return (
    <div className="grid gap-2">
      <label htmlFor="add-project-path" className="text-ui-base font-medium">
        Path
      </label>
      <div className="flex gap-2">
        <Input
          id="add-project-path"
          data-testid="add-project-path-input"
          placeholder="/path/to/folder"
          value={value.path}
          onChange={(e) => onChange({ ...value, path: e.target.value })}
          onBlur={() => void onCheck(value.path)}
          autoFocus
          className="flex-1"
        />
        <Button
          type="button"
          variant="outline"
          data-testid="add-project-browse-button"
          onClick={() => setPickerOpen(true)}
        >
          Browse…
        </Button>
      </div>
      <div data-testid="add-project-path-status" aria-live="polite">
        {status.kind === "not-git" && (
          <p data-testid="add-project-hint-not-git" className="text-ui-base text-foreground-muted">
            Folders without git are not supported yet.
          </p>
        )}
        {status.kind === "missing" && (
          <p data-testid="add-project-hint-missing" className="text-ui-base text-foreground-muted">
            This folder does not exist on {hostName}.
          </p>
        )}
        {status.kind === "relative" && (
          <p data-testid="add-project-hint-relative" className="text-ui-base text-foreground-muted">
            Use an absolute path, or start with ~.
          </p>
        )}
        {status.kind === "duplicate" && (
          <p data-testid="add-project-hint-duplicate" className="flex flex-wrap items-center gap-2 text-ui-base text-foreground-muted">
            Already a project: {status.workspace.Title}.
            <Button
              type="submit"
              variant="quiet"
              size="xs"
              data-testid="add-project-open-existing"
              className="h-auto py-0.5"
            >
              Open it
            </Button>
          </p>
        )}
        {status.kind === "git" && (
          <p className="flex items-center gap-1.5 text-ui-base text-foreground-muted">
            <Icons.workspace className="size-3.5 shrink-0" />
            Git repository.
          </p>
        )}
      </div>
      {recents.length > 0 && (
        <div data-testid="add-project-recents" className="overflow-hidden rounded-xl border border-border">
          <p className="border-b border-border px-3 py-1.5 text-ui-sm font-medium text-foreground-muted">Recent</p>
          <ul>
            {recents.map((recent, index) => (
              <li key={recent.path} className="flex items-center border-b border-border last:border-b-0">
                <button
                  type="button"
                  data-testid={`add-project-recent-${index}`}
                  onClick={() => setPath(recent.path)}
                  className="flex min-w-0 flex-1 items-baseline gap-2 px-3 py-1.5 text-left hover:bg-hover"
                >
                  <span className="min-w-0 truncate text-ui-base text-foreground">{recent.title}</span>
                  <span className="min-w-0 truncate text-ui-sm text-foreground-subtle">{recent.path}</span>
                </button>
                <button
                  type="button"
                  aria-label={`Remove ${recent.path} from recents`}
                  data-testid={`add-project-recent-remove-${index}`}
                  onClick={() => {
                    removeRecent(hostKey, recent.path);
                    bumpRecents((n) => n + 1);
                  }}
                  className={cn(
                    "mr-2 shrink-0 rounded-md p-1 text-foreground-subtle outline-none hover:bg-hover hover:text-foreground",
                    "focus-visible:ring-3 focus-visible:ring-ring/50",
                  )}
                >
                  <Icons.close className="size-3.5" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div>
        <button
          type="button"
          data-testid="add-project-name-toggle"
          aria-expanded={nameOpen}
          onClick={() => setNameOpen(!nameOpen)}
          className="flex items-center gap-1 text-ui-sm text-foreground-muted outline-none focus-visible:ring-3 focus-visible:ring-ring/50 rounded-md"
        >
          <Icons.chevronDown
            aria-hidden
            className={cn("size-3.5 shrink-0 transition-transform", nameOpen ? "" : "-rotate-90")}
          />
          Name (optional)
        </button>
        {nameOpen && (
          <Input
            id="add-project-name"
            data-testid="add-project-name-input"
            placeholder="Defaults to the repository directory name"
            value={value.name}
            onChange={(e) => onChange({ ...value, name: e.target.value })}
            className="mt-2"
          />
        )}
      </div>
      <FolderPickerDialog
        client={client}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onSelect={(selected) => setPath(selected)}
        recentPaths={recentPaths}
      />
    </div>
  );
}
