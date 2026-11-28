import { FormError } from "@/components/ui/form-error";

import { SegmentedControl } from "@/components/ui/segmented-control";
import { useSetLearningMode } from "@/hooks/use-learning";
import type { LearningMode, LearningWorkspaceStatus } from "@/lib/learning-types";

const MODE_OPTIONS: { value: LearningMode; label: string }[] = [
  { value: "off", label: "Off" },
  { value: "propose", label: "Propose" },
];

/**
 * The workspace's opt-in: `off` (default) or `propose`. The subtitle says what turning it on
 * means, so consent is informed: excerpts of transcripts go to the model, and nothing a run
 * learns is used until the person approves it. Disabled while the global switch (shown above it, in Settings) is off.
 */
export function LearningOptIn({
  workspace,
  enabled,
}: {
  workspace: LearningWorkspaceStatus;
  /** `status.enabled`: false is the global kill switch. */
  enabled: boolean;
}) {
  const setMode = useSetLearningMode();
  return (
    <div className="flex flex-col gap-2" data-testid="learning-optin">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-ui-base font-medium text-foreground">Learn from {workspace.title}</h3>
          <p data-testid="learning-optin-subtitle" className="text-ui-sm text-muted-foreground">
            Excerpts of this workspace&apos;s transcripts are sent to the model to spot reusable patterns. Skills it finds
            are proposals: none is used until you approve it.
          </p>
        </div>
        <SegmentedControl
          label="Learning mode"
          options={MODE_OPTIONS}
          value={workspace.learningMode}
          onChange={(mode) => setMode.mutate({ workspaceId: workspace.id, mode })}
          disabled={!enabled || setMode.isPending}
        />
      </div>
      {setMode.error && (
        <FormError message={setMode.error.message} testId="learning-optin-error" size="sm" />
      )}
    </div>
  );
}
