import type { ForgeSearchItem } from "@/hooks/use-forge-search";
import type { WorkspaceBranch } from "@/lib/types";

export type StartMode = "head" | "branch" | "pr" | "issue";

/** A forge item the user picked, with the repo the search ran against. */
export interface PickedForgeItem {
  item: ForgeSearchItem;
  repo: string;
}

/** How a picked branch becomes the task: a new branch off it (`baseRef`), or the branch itself (`branch`). */
export type BranchUse = "baseRef" | "checkout";

/** The New Task dialog's "Start from" choice: one object, so switching mode drops the other modes' picks. */
export interface StartFromState {
  mode: StartMode;
  branch?: WorkspaceBranch;
  branchUse: BranchUse;
  pr?: PickedForgeItem;
  issue?: PickedForgeItem;
}

export type StartFromAction =
  | { type: "mode"; mode: StartMode }
  | { type: "branch"; branch: WorkspaceBranch | undefined }
  | { type: "branchUse"; use: BranchUse }
  | { type: "pr"; pr: PickedForgeItem | undefined }
  | { type: "issue"; issue: PickedForgeItem | undefined };

export const INITIAL_START_FROM: StartFromState = { mode: "head", branchUse: "baseRef" };

export function startFromReducer(state: StartFromState, action: StartFromAction): StartFromState {
  switch (action.type) {
    case "mode":
      return action.mode === state.mode ? state : { mode: action.mode, branchUse: state.branchUse };
    case "branch":
      return { ...state, branch: action.branch };
    case "branchUse":
      return { ...state, branchUse: action.use };
    case "pr":
      return { ...state, pr: action.pr };
    case "issue":
      return { ...state, issue: action.issue };
  }
}

/** What the mode still needs before Create: null when complete. */
export function startFromMissing(state: StartFromState): string | null {
  if (state.mode === "branch" && !state.branch) return "Choose a branch.";
  if (state.mode === "pr" && !state.pr) return "Choose a pull request.";
  if (state.mode === "issue" && !state.issue) return "Choose an issue.";
  return null;
}

/** The (at most one) `task.create` ref param for the state; `{}` for Workspace HEAD and for an issue (a plain task). */
export function startFromParams(state: StartFromState): { baseRef?: string; branch?: string; prNumber?: number } {
  switch (state.mode) {
    case "branch": {
      const b = state.branch;
      if (!b) return {};
      // The daemon lists an origin-only branch by its short name; `baseRef` wants the remote ref, `branch` the short name.
      if (state.branchUse === "checkout") return { branch: b.name };
      return { baseRef: b.remote === "origin" ? `origin/${b.name}` : b.name };
    }
    case "pr":
      return state.pr ? { prNumber: state.pr.item.number } : {};
    default:
      return {};
  }
}
