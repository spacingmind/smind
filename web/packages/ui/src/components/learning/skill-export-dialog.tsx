import { useState } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useExportSkill } from "@/hooks/use-learning";
import type { ExportResult, ExportTarget, LearnedSkillView } from "@/lib/learning-types";

const TARGETS: { value: ExportTarget; label: string }[] = [
  { value: "repo", label: "This repository (its skills folder)" },
  { value: "claude", label: "~/.claude/skills" },
  { value: "agents", label: "~/.agents/skills" },
  { value: "codex", label: "~/.codex/skills" },
];

/** What came back, in one calm block: the files written, what was left alone, or that nothing changed. */
function ExportOutcome({ result }: { result: ExportResult }) {
  if (result.status === "modified") {
    return (
      <Alert
        variant="warning"
        testId="skill-export-modified"
        description={
          <>
            A different SKILL.md is already there and was left alone:
            <ul className="mt-1 flex flex-col gap-0.5 font-mono text-ui-sm break-all">
              {result.skipped.map((file) => (
                <li key={file}>{file}</li>
              ))}
            </ul>
          </>
        }
      />
    );
  }
  if (result.files.length === 0) {
    return (
      <p data-testid="skill-export-uptodate" className="text-ui-base text-muted-foreground">
        Already up to date
      </p>
    );
  }
  return (
    <div data-testid="skill-export-result" className="flex flex-col gap-1 rounded-lg border bg-card p-3 text-ui-sm">
      <span className="text-foreground-subtle">Exported to</span>
      <span data-testid="skill-export-dir" className="select-all break-all font-mono text-foreground">
        {result.dir}
      </span>
      <ul className="flex flex-col gap-0.5">
        {result.files.map((file) => (
          <li key={file} data-testid="skill-export-file" className="select-all break-all font-mono text-foreground-muted">
            {file}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** The form lives inside the dialog content, so closing it drops the chosen target, the result and any error. */
function ExportForm({ skill, onClose }: { skill: LearnedSkillView; onClose: () => void }) {
  const exportSkill = useExportSkill();
  const [target, setTarget] = useState<ExportTarget>("repo");
  const [force, setForce] = useState(false);
  const pending = exportSkill.isPending;
  const result = exportSkill.data;
  // Replace is offered only once the daemon has said it left an edited copy alone.
  const canReplace = result?.status === "modified";

  const choose = (next: ExportTarget) => {
    setTarget(next);
    setForce(false);
    exportSkill.reset();
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>Export {skill.name}</DialogTitle>
        <DialogDescription>
          Writes SKILL.md. After export the file is yours: smind stops managing it, so later edits and approvals here do not
          change it.
        </DialogDescription>
      </DialogHeader>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1.5 text-ui-sm text-foreground-subtle">Write it to</legend>
        {TARGETS.map((t) => (
          <label key={t.value} className="flex items-center gap-2 text-ui-base text-foreground">
            <input
              type="radio"
              name="skill-export-target"
              value={t.value}
              checked={target === t.value}
              disabled={pending}
              onChange={() => choose(t.value)}
            />
            {t.label}
          </label>
        ))}
      </fieldset>
      <div className="flex min-h-32 min-w-0 flex-col gap-2 [overflow-wrap:anywhere]" aria-live="polite">
        {result && <ExportOutcome result={result} />}
        {canReplace && (
          <label className="flex items-center gap-2 text-ui-sm text-foreground-muted">
            <Checkbox
              data-testid="skill-export-force"
              checked={force}
              disabled={pending}
              onCheckedChange={(on) => setForce(on === true)}
            />
            Replace an edited copy
          </label>
        )}
        {exportSkill.error && <Alert variant="error" testId="skill-export-error" description={exportSkill.error.message.replace(/^learning\.skills\.export:\s*/, "")} />}
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={pending} data-testid="skill-export-cancel" onClick={onClose}>
          {result && result.status !== "modified" ? "Close" : "Cancel"}
        </Button>
        <Button
          type="button"
          disabled={pending}
          data-testid="skill-export-submit"
          onClick={() => exportSkill.mutate({ id: skill.id, target, ...(canReplace && force ? { force: true } : {}) })}
        >
          {pending ? "Exporting…" : "Export"}
        </Button>
      </DialogFooter>
    </>
  );
}

/** Export a learned skill's SKILL.md to a repository or a user skills directory (ADR-0026). */
export function SkillExportDialog({
  skill,
  open,
  onOpenChange,
}: {
  skill: LearnedSkillView;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="skill-export-dialog">
        <ExportForm skill={skill} onClose={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}
