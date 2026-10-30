import { describe, expect, it } from "vitest";

import { Icons } from "@/lib/icons";

// Every source file, as text. Vite resolves the glob at transform time, so the
// test needs no fs access.
const SOURCES = import.meta.glob<string>("/src/**/*.{ts,tsx}", { query: "?raw", import: "default", eager: true });

/** Files that may import lucide-react directly: the registry itself and the file-type glyph table (a domain of its own, not an app concept). */
const ALLOWED = new Set(["/src/lib/icons.ts", "/src/lib/file-icons.tsx"]);

describe("icon registry", () => {
  it("binds each concept to the intended lucide glyph (change a row on purpose, not by drift)", () => {
    const glyphs = Object.fromEntries(Object.entries(Icons).map(([name, icon]) => [name, icon.displayName]));
    expect(glyphs).toMatchInlineSnapshot(`
      {
        "add": "Plus",
        "addComment": "MessageSquarePlus",
        "agent": "Bot",
        "appearance": "Palette",
        "archive": "Archive",
        "arrowRight": "ArrowRight",
        "attach": "Paperclip",
        "automation": "CalendarClock",
        "back": "ArrowLeft",
        "branch": "GitBranch",
        "chat": "MessageSquare",
        "check": "Check",
        "checkAll": "CheckCheck",
        "chevronDown": "ChevronDown",
        "chevronRight": "ChevronRight",
        "chevronUp": "ChevronUp",
        "clock": "Clock",
        "close": "X",
        "commit": "GitCommitHorizontal",
        "connections": "Network",
        "copy": "Copy",
        "credentials": "KeyRound",
        "defaultAgent": "Star",
        "delete": "Trash2",
        "diff": "GitCompare",
        "down": "ArrowDown",
        "download": "Download",
        "draft": "Sparkles",
        "edit": "Pencil",
        "error": "CircleAlert",
        "externalLink": "ExternalLink",
        "file": "File",
        "fileCode": "FileCode",
        "fileEdit": "FileDiff",
        "fileText": "FileText",
        "files": "FolderTree",
        "folder": "Folder",
        "folderAdd": "FolderPlus",
        "folderOpen": "FolderOpen",
        "forge": "GitFork",
        "groupByStage": "Kanban",
        "groupByStatus": "ListChecks",
        "groupByWorkspace": "FolderGit2",
        "help": "CircleQuestionMark",
        "host": "Server",
        "image": "Image",
        "inbox": "Inbox",
        "info": "Info",
        "issue": "CircleDot",
        "link": "Link2",
        "lock": "Lock",
        "mcp": "Plug",
        "mention": "AtSign",
        "menu": "Menu",
        "merge": "GitMerge",
        "model": "Cpu",
        "more": "Ellipsis",
        "notifications": "Bell",
        "openInEditor": "Code",
        "openToSide": "PanelRight",
        "options": "SlidersHorizontal",
        "paste": "ClipboardPaste",
        "permission": "ShieldCheck",
        "permissionDanger": "ShieldAlert",
        "pin": "Pin",
        "play": "Play",
        "plugin": "Puzzle",
        "preview": "Eye",
        "providerGeneric": "Blocks",
        "pullRequest": "GitPullRequest",
        "push": "Upload",
        "question": "CircleQuestionMark",
        "refresh": "RefreshCw",
        "reset": "Undo2",
        "retry": "RotateCcw",
        "search": "Search",
        "send": "ArrowUp",
        "sendNow": "Zap",
        "settings": "Settings",
        "shortcuts": "Keyboard",
        "sidebarToggle": "PanelLeft",
        "skills": "BookOpen",
        "space": "Layers",
        "spinner": "LoaderCircle",
        "splitColumns": "Columns2",
        "splitRows": "Rows2",
        "stalled": "Hourglass",
        "statusIdle": "Circle",
        "statusSkipped": "CircleMinus",
        "stop": "Square",
        "success": "CircleCheck",
        "terminal": "Terminal",
        "themeDark": "Moon",
        "themeLight": "Sun",
        "themeSystem": "Monitor",
        "thinking": "Brain",
        "time": "Clock",
        "tool": "Wrench",
        "toolGroup": "Boxes",
        "unpin": "PinOff",
        "up": "ArrowUp",
        "usage": "Gauge",
        "verdict": "ClipboardCheck",
        "warning": "TriangleAlert",
        "web": "Globe",
        "workspace": "FolderGit2",
      }
    `);
  });

  it("is imported through, not around: no component imports lucide-react directly", () => {
    const offenders = Object.entries(SOURCES)
      .filter(([path]) => !ALLOWED.has(path) && !/\.test\.tsx?$/.test(path))
      .filter(([, text]) => /["']lucide-react(?:\/[^"']*)?["']/.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  it("uses no deprecated lucide alias names in the allowed files", () => {
    const alias = /\b(AlertCircle|AlertTriangle|CheckCircle2|Loader2|MoreHorizontal|MoreVertical|HelpCircle|CircleHelp|BarChart3)\b/;
    for (const path of ALLOWED) {
      const code = SOURCES[path]!.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
      expect(code, path).not.toMatch(alias);
    }
  });
});
