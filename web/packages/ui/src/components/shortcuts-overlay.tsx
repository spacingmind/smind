import { useState } from "react";

import { ShortcutRows } from "@/components/shortcuts-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Icons } from "@/lib/icons";

/**
 * The `?` cheat-sheet (AC2): a searchable, read-only shortcuts list over
 * whatever view is showing, instead of navigating to Settings. Rebinding
 * stays in Settings -- the footer link deep-links there.
 */
export function ShortcutsOverlay({
  open,
  onOpenChange,
  onCustomize,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** "Customize in Settings": closes the overlay and opens Settings' Shortcuts section. */
  onCustomize: () => void;
}) {
  const [query, setQuery] = useState("");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="shortcuts-overlay" className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>Search the shortcuts available in this app.</DialogDescription>
        </DialogHeader>
        <DialogBody>
          <div className="relative">
            <Icons.search
              aria-hidden
              className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-foreground-muted"
            />
            <Input
              type="search"
              aria-label="Search shortcuts"
              placeholder="Search shortcuts…"
              data-testid="shortcuts-overlay-search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full pl-8"
            />
          </div>
          <div className="max-h-[60vh] overflow-y-auto">
            <ShortcutRows query={query} readOnly />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button
            variant="link"
            size="sm"
            onClick={() => {
              onOpenChange(false);
              onCustomize();
            }}
          >
            Customize in Settings
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
