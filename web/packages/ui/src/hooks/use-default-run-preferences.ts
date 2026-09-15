import { useCallback, useState } from "react";

import {
  readStoredDefaultApprovalPolicy,
  readStoredDefaultProvider,
  writeStoredDefaultApprovalPolicy,
  writeStoredDefaultProvider,
} from "@/lib/settings-preferences";
import type { ApprovalPolicy, Provider } from "@/lib/types";

/**
 * The composer's default-provider and default-approval-policy preferences
 * (ui-redesign-parity plan, Item 13's General section). Reading and
 * setting both live here, in one hook, rather than in the settings
 * screen's own component: the composer (task-detail.tsx, Track B) is the
 * other consumer this hook exists for -- "the default-provider preference
 * is applied to a newly opened composer" is Item 13's acceptance
 * criterion, but composer.tsx is outside this track's file ownership
 * (see the plan's Tracks section), so this hook is the seam. It reads
 * once per mount, same as useTheme/useNotificationPermission's
 * lazy-initial-state shape; a change made in one open settings screen and
 * a composer mounted before that change simply doesn't retroactively
 * apply, matching how a composer already open doesn't re-read any other
 * default when a preference changes elsewhere.
 */
export function useDefaultRunPreferences(): {
  defaultProvider: Provider | null;
  setDefaultProvider: (provider: Provider | null) => void;
  defaultApprovalPolicy: ApprovalPolicy | null;
  setDefaultApprovalPolicy: (policy: ApprovalPolicy | null) => void;
} {
  const [defaultProvider, setDefaultProviderState] = useState<Provider | null>(readStoredDefaultProvider);
  const [defaultApprovalPolicy, setDefaultApprovalPolicyState] = useState<ApprovalPolicy | null>(
    readStoredDefaultApprovalPolicy,
  );

  const setDefaultProvider = useCallback((provider: Provider | null) => {
    setDefaultProviderState(provider);
    writeStoredDefaultProvider(provider);
  }, []);

  const setDefaultApprovalPolicy = useCallback((policy: ApprovalPolicy | null) => {
    setDefaultApprovalPolicyState(policy);
    writeStoredDefaultApprovalPolicy(policy);
  }, []);

  return { defaultProvider, setDefaultProvider, defaultApprovalPolicy, setDefaultApprovalPolicy };
}
