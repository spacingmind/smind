import { NoticeBanner } from "@/components/ui/notice-banner";
import { useQuotaNotice } from "@/hooks/use-quota-notice";

/**
 * The ambient quota notice (ADR-0059 F): a dismissible, full-width row in
 * the composer's normal flow above the input card when one of the
 * composer provider's plan windows is at or over 90 % used. It renders
 * nothing without a notice, and reads only the shared cached usage
 * reports -- never a touch, a refresh or any other account.usage request.
 */
export function AmbientQuotaNotice({ provider }: { provider: string }) {
  const { notice, dismiss } = useQuotaNotice(provider);
  if (!notice) return null;
  return (
    <NoticeBanner
      tone={notice.level === "full" ? "error" : "warning"}
      testId="ambient-quota-notice"
      onDismiss={dismiss}
      dismissLabel="Dismiss the quota notice"
      dismissTestId="ambient-quota-dismiss"
    >
      <span data-testid="ambient-quota-text">{notice.text}</span>
    </NoticeBanner>
  );
}
