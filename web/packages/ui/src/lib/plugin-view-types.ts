// The shape of a daemon-validated ui:1 plugin view document, as the web
// renderer consumes it (internal/plugins/uiview/schema.go is the authority:
// the daemon has already validated and normalised the document before it
// reaches the client, so these are trust-boundary-free presentation types).
export type PluginTone = "success" | "warning" | "danger" | "muted";

/** A curated lucide name (^[A-Z][A-Za-z0-9]*$) or an inline svg literal. */
export type PluginIcon = string | { svg: string };

export type PluginViewAction =
  | { kind: "rpc"; rpc: string; params?: Record<string, unknown> }
  | { kind: "prompt"; text: string }
  | { kind: "command"; name: string }
  | { kind: "openPanel"; panel: string; context?: Record<string, unknown> }
  /** `host` is added by the daemon (sanitizeOpenURL), never by the plugin. */
  | { kind: "openUrl"; url: string; host: string };

/** Extra payload a caller receives with an action (form submit values). */
export type PluginActionExtra = { formValues?: Record<string, string | boolean> };

export type PluginViewDoc = {
  ui: number;
  title?: string;
  root: unknown;
};
