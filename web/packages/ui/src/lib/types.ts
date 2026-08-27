// Wire types for internal/wsapi's workspace.*/task.* results. These mirror
// internal/store.Workspace/Task field-for-field, including their exact
// (PascalCase) JSON keys -- those types carry no `json:` tags, so
// encoding/json marshals them using the Go field names verbatim.

export interface Workspace {
  ID: number;
  Path: string;
  Title: string;
  RoutingPolicy: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Task {
  ID: number;
  WorkspaceID: number;
  SpaceID: number | null;
  Title: string;
  Status: string;
  WorktreePath: string | null;
  Branch: string | null;
  CreatedAt: string;
  UpdatedAt: string;
  ArchivedAt: string | null;
}
