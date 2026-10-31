import type { IconName } from "@/lib/icons";

/**
 * What the palette's category chips offer, and what each group renders as.
 *
 * Groups are free-form strings ("Tasks", "Elsewhere", anything a source
 * names); chips need the fixed few. Every group maps onto one category,
 * unknown ones included, so a new source shows up under Actions rather
 * than not at all.
 */
export type PaletteCategory = "all" | "tasks" | "actions" | "files";

export const PALETTE_CATEGORY_LABEL: Record<PaletteCategory, string> = {
  all: "All",
  tasks: "Tasks",
  actions: "Actions",
  files: "Files",
};

export function paletteCategory(group: string): Exclude<PaletteCategory, "all"> {
  if (group === "Tasks" || group === "Workspaces" || group === "Chats") return "tasks";
  if (group === "Files") return "files";
  return "actions";
}

export const PALETTE_GROUP_ICON: Record<string, IconName> = {
  Tasks: "chat",
  Workspaces: "folder",
  Open: "openToSide",
  Chats: "chat",
  Files: "file",
  Actions: "play",
  Commands: "terminal",
  Settings: "settings",
  Agents: "agent",
  Plugins: "plugin",
};

export function iconForGroup(group: string): IconName {
  return PALETTE_GROUP_ICON[group] ?? "play";
}

export const PALETTE_CATEGORY_ICON: Record<PaletteCategory, IconName> = {
  all: "menu",
  tasks: "chat",
  actions: "play",
  files: "file",
};
