import { useState } from "react";

import { Button } from "@/components/ui/button";
import { NoticeAction, NoticeBanner } from "@/components/ui/notice-banner";
import { useRolePackCheck } from "@/hooks/use-role-pack";
import { bannerText, currentLine } from "@/lib/role-pack-text";

/**
 * The role-pack banner (docs/plans/active/role-pack-sync.md, PR 2): the one line above the profile list
 * that says whether the pack's role prompts drifted (AC12). Read-only -- it never calls apply; only the
 * review dialog's buttons do. An answer without data (an older daemon) renders nothing.
 */
export function RolePackBanner() {
  const check = useRolePackCheck();
  const [reviewOpen, setReviewOpen] = useState(false);

  if (check.isError) {
    const message = check.error instanceof Error ? check.error.message : String(check.error);
    return (
      <div data-testid="role-pack-error" role="alert" className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
        <span>Could not check role prompts: {message}</span>
        <Button type="button" size="sm" variant="outline" data-testid="role-pack-retry" onClick={() => void check.refetch()}>
          Retry
        </Button>
      </div>
    );
  }

  const data = check.data;
  if (data === null || data === undefined) return null;

  if (data.seatsWithUpdates > 0) {
    return (
      <>
        <NoticeBanner
          tone="info"
          testId="role-pack-banner"
          actions={
            <NoticeAction data-testid="role-pack-review-open" onClick={() => setReviewOpen(true)}>
              Review
            </NoticeAction>
          }
        >
          {bannerText(data.seatsWithUpdates)}
        </NoticeBanner>
        {reviewOpen ? null : null}
      </>
    );
  }

  return (
    <p data-testid="role-pack-current" className="text-ui-sm text-foreground-muted">
      {currentLine(data.packVersion)}
    </p>
  );
}
