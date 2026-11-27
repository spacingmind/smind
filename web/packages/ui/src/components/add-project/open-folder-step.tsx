import { useState } from "react";

import { Button } from "@/components/ui/button";
import { FolderPickerDialog } from "@/components/folder-picker-dialog";
import { Input } from "@/components/ui/input";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";

/** What the dialog reads out of this step: the typed (or picked) path and the optional title. */
export interface OpenFolderValue {
  path: string;
  name: string;
}

/**
 * The "Add project" dialog's details for the one live method (C1 D3): a path
 * input with the in-app folder chooser ("Browse…"), and the optional Name
 * collapsed behind its own disclosure (D8). Controlled by the dialog: the
 * value lives there so submit and the error row see the same state.
 */
export function OpenFolderStep({
  client,
  value,
  onChange,
  recentPaths = [],
}: {
  client: WsClient | null;
  value: OpenFolderValue;
  onChange: (next: OpenFolderValue) => void;
  /** Parent directories of already-registered workspaces, for the folder picker's quick-jump row. */
  recentPaths?: string[];
}) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const [nameOpen, setNameOpen] = useState(false);

  return (
    <div className="grid gap-2">
      <label htmlFor="add-project-path" className="text-ui-base font-medium">
        Path
      </label>
      <div className="flex gap-2">
        <Input
          id="add-project-path"
          data-testid="add-project-path-input"
          placeholder="/path/to/repo"
          value={value.path}
          onChange={(e) => onChange({ ...value, path: e.target.value })}
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
        onSelect={(selected) => onChange({ ...value, path: selected })}
        recentPaths={recentPaths}
      />
    </div>
  );
}
