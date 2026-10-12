import { useCallback, useEffect, useReducer, useRef } from "react";

import { toast } from "@/components/ui/toast";
import { desktop, isDesktop, type AppUpdateCheck, type AppUpdateProgress } from "@/lib/platform";

/** The `available` variant of `AppUpdateCheck`, the only one the UI ever shows. */
export interface AppUpdateInfo {
  version: string;
  currentVersion: string;
  notes: string | null;
  date: string | null;
}

/**
 * ADR-0023 U4: idle -> prompt (user confirms before anything is downloaded)
 * -> downloading (progress events) -> installing; an install failure returns
 * to prompt with the error inline, so Update can be retried.
 */
export type AppUpdateState =
  /** `available` is an update the user has not acted on (dismissed with Later): the header badge's source, and what `reopen` prompts with. */
  | { stage: "idle"; available?: AppUpdateInfo }
  | { stage: "prompt"; info: AppUpdateInfo; error: string | null }
  | { stage: "downloading"; info: AppUpdateInfo; progress: AppUpdateProgress }
  | { stage: "installing"; info: AppUpdateInfo };

export type AppUpdateAction =
  | { type: "checkResult"; check: AppUpdateCheck; manual: boolean; dismissed: ReadonlySet<string> }
  | { type: "dismiss" }
  | { type: "reopen" }
  | { type: "confirm" }
  | { type: "progress"; progress: AppUpdateProgress }
  | { type: "installError"; message: string };

export const initialAppUpdateState: AppUpdateState = { stage: "idle" };

export function appUpdateReducer(state: AppUpdateState, action: AppUpdateAction): AppUpdateState {
  switch (action.type) {
    case "checkResult": {
      // Never yank an in-flight download out from under the user.
      if (state.stage === "downloading" || state.stage === "installing") return state;
      if (action.check.kind !== "available") return { stage: "idle" };
      const info: AppUpdateInfo = {
        version: action.check.version,
        currentVersion: action.check.currentVersion,
        notes: action.check.notes,
        date: action.check.date,
      };
      // A version dismissed this session isn't re-prompted by the launch
      // check (the header badge still offers it); a manual check always asks again.
      if (!action.manual && action.dismissed.has(action.check.version)) return { stage: "idle", available: info };
      return { stage: "prompt", info, error: null };
    }
    case "dismiss":
      // Later keeps the update for the badge. Dismissing mid-download only hides
      // the dialog (the install carries on), so there is nothing to offer again.
      if (state.stage === "prompt") return { stage: "idle", available: state.info };
      return state.stage === "idle" ? state : { stage: "idle" };
    case "reopen":
      return state.stage === "idle" && state.available ? { stage: "prompt", info: state.available, error: null } : state;
    case "confirm": {
      if (state.stage !== "prompt") return state;
      return {
        stage: "downloading",
        info: state.info,
        progress: { stage: "downloading", downloaded: 0, total: null },
      };
    }
    case "progress": {
      if (state.stage !== "downloading" && state.stage !== "installing") return state;
      if (action.progress.stage === "installing") return { stage: "installing", info: state.info };
      return { stage: "downloading", info: state.info, progress: action.progress };
    }
    case "installError": {
      if (state.stage === "idle" || state.stage === "prompt") return state;
      return { stage: "prompt", info: state.info, error: action.message };
    }
  }
}

/** What `useAppUpdate` returns: the dialog and the header badge share one instance (the launch check must run once). */
export type AppUpdateController = ReturnType<typeof useAppUpdate>;

/** The update the badge offers: one waiting for a decision (prompt, or dismissed with Later). Null while downloading/installing or when there is none. */
export function pendingUpdate(state: AppUpdateState): AppUpdateInfo | null {
  if (state.stage === "prompt") return state.info;
  if (state.stage === "idle") return state.available ?? null;
  return null;
}

/**
 * Owns the update flow's effects: one background launch check (errors
 * swallowed, unsupported/upToDate silent), the Check for Updates… menu
 * event (toasts on up-to-date/error, always re-prompts on available),
 * install progress events, and the confirm/dismiss actions the dialog calls.
 */
export function useAppUpdate() {
  const [state, dispatch] = useReducer(appUpdateReducer, initialAppUpdateState);
  const dismissedRef = useRef<Set<string>>(new Set());
  // A ref, not effect deps: the launch check runs exactly once per session,
  // including across a StrictMode remount.
  const launchedRef = useRef(false);
  const stateRef = useRef(state);
  stateRef.current = state;

  const runCheck = useCallback((manual: boolean) => {
    desktop
      .appUpdateCheck()
      .then((check) => {
        if (check.kind === "upToDate" && manual) {
          toast({ variant: "success", title: `smind is up to date (v${check.currentVersion})` });
        }
        dispatch({ type: "checkResult", check, manual, dismissed: dismissedRef.current });
      })
      .catch((err: unknown) => {
        // Being offline at launch is normal; only a manual check surfaces it.
        if (manual) {
          toast({ variant: "error", title: "Update check failed", description: err instanceof Error ? err.message : String(err) });
        } else {
          console.debug("launch update check failed", err);
        }
      });
  }, []);

  useEffect(() => {
    if (!isDesktop || launchedRef.current) return;
    launchedRef.current = true;
    runCheck(false);
  }, [runCheck]);

  useEffect(() => {
    if (!isDesktop) return;
    return desktop.onAppUpdateCheckRequested(() => runCheck(true));
  }, [runCheck]);

  useEffect(() => {
    if (!isDesktop) return;
    return desktop.onAppUpdateProgress((progress) => dispatch({ type: "progress", progress }));
  }, []);

  const confirm = useCallback(() => {
    if (!isDesktop) return;
    dispatch({ type: "confirm" });
    desktop
      .appUpdateInstall()
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        dispatch({ type: "installError", message });
        toast({ variant: "error", title: "Update failed", description: message });
      });
  }, []);

  const dismiss = useCallback(() => {
    if (stateRef.current.stage !== "idle") dismissedRef.current.add(stateRef.current.info.version);
    dispatch({ type: "dismiss" });
  }, []);

  const reopen = useCallback(() => dispatch({ type: "reopen" }), []);

  return { state, confirm, dismiss, reopen };
}
