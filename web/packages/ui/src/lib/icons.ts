// The one place a lucide icon is bound to an app concept. Components import
// `Icons` (and `ICON_SIZE`) from here, never from "lucide-react" directly
// (icons.test.ts enforces it), so the same concept renders the same glyph
// everywhere and a swap is a one-line change.
//
// Names are by *meaning*, not by glyph: `Icons.retry`, not `Icons.rotateCcw`.
// Lucide's deprecated alias names (AlertCircle, AlertTriangle, CheckCircle2,
// Loader2, MoreHorizontal, `*Icon` suffixes) are the same glyphs as the
// canonical ones below; the registry uses canonical names only.
//
// Glyph choices follow ZCode / Paseo where they have one (Gauge, Settings,
// Ellipsis, ExternalLink, Undo2 for reset, CircleAlert / TriangleAlert /
// CircleCheck for states, Bot for agents, Brain for thinking, Pencil for
// edit); terminal is `>_` per the app-header-and-tabs plan (D8). Provider brand marks live in
// components/provider-icon.tsx, file-type glyphs in lib/file-icons.tsx.
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Archive,
  AtSign,
  Bell,
  Blocks,
  BookOpen,
  Boxes,
  Bot,
  Brain,
  Check,
  CheckCheck,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Circle,
  CircleAlert,
  CircleCheck,
  CircleDot,
  CircleMinus,
  CircleQuestionMark,
  ClipboardPaste,
  Clock,
  Code,
  Columns2,
  Copy,
  Cpu,
  Download,
  Ellipsis,
  ExternalLink,
  Eye,
  File,
  FileCode,
  FileDiff,
  FileText,
  Folder,
  FolderGit2,
  FolderOpen,
  FolderPlus,
  FolderTree,
  Gauge,
  GitBranch,
  GitFork,
  GitCommitHorizontal,
  GitCompare,
  GitMerge,
  GitPullRequest,
  Globe,
  Hourglass,
  Link2,
  Image,
  Info,
  KeyRound,
  Kanban,
  Keyboard,
  Layers,
  ListChecks,
  LoaderCircle,
  Menu,
  MessageSquare,
  MessageSquarePlus,
  Monitor,
  Moon,
  Network,
  Palette,
  Paperclip,
  PanelLeft,
  PanelRight,
  Pencil,
  Pin,
  PinOff,
  Plug,
  Plus,
  Puzzle,
  RefreshCw,
  RotateCcw,
  Rows2,
  Search,
  Server,
  Settings,
  Lock,
  Play,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Square,
  SquarePen,
  Sparkles,
  Star,
  Sun,
  Terminal,
  Trash2,
  TriangleAlert,
  Undo2,
  Upload,
  Wrench,
  X,
  Zap,
  CalendarClock,
  ClipboardCheck,
  Inbox,
  type LucideIcon,
} from "lucide-react";

export type IconComponent = LucideIcon;

export const Icons = {
  // --- actions
  add: Plus,
  /** New task quick action (sidebar). */
  newTask: SquarePen,
  close: X,
  check: Check,
  checkAll: CheckCheck,
  copy: Copy,
  paste: ClipboardPaste,
  edit: Pencil,
  delete: Trash2,
  archive: Archive,
  pin: Pin,
  unpin: PinOff,
  search: Search,
  /** Setup blocked until the user runs it (a fork PR, ADR-0039 §7). */
  lock: Lock,
  /** Run a script the user chose to run (Run setup). */
  play: Play,
  /** Re-fetch data (usage, lists). */
  refresh: RefreshCw,
  /** Run / step again after a failure or on request. */
  retry: RotateCcw,
  /** Put a value back to its saved default. */
  reset: Undo2,
  /** Overflow ("...") menu trigger. */
  more: Ellipsis,
  externalLink: ExternalLink,
  download: Download,
  send: ArrowUp,
  stop: Square,
  /** Interrupt the running turn and send now. */
  sendNow: Zap,
  menu: Menu,
  sidebarToggle: PanelLeft,
  addComment: MessageSquarePlus,
  /** Split left / right. */
  splitColumns: Columns2,
  /** Split up / down. */
  splitRows: Rows2,
  openToSide: PanelRight,
  preview: Eye,
  openInEditor: Code,

  // --- navigation / direction
  back: ArrowLeft,
  arrowRight: ArrowRight,
  up: ArrowUp,
  down: ArrowDown,
  chevronRight: ChevronRight,
  chevronDown: ChevronDown,
  chevronUp: ChevronUp,

  // --- app concepts
  settings: Settings,
  /** Adjustable run options (as opposed to app settings). */
  options: SlidersHorizontal,
  shortcuts: Keyboard,
  help: CircleQuestionMark,
  /** The agent asking the user a question (same "?" mark as help). */
  question: CircleQuestionMark,
  /** `>_` -- the Terminal tab, header button, bash tool and worktree scripts (app-header-and-tabs D8). */
  terminal: Terminal,
  chat: MessageSquare,
  files: FolderTree,
  diff: GitCompare,
  /** A file-edit tool call (distinct from the diff *view*). */
  fileEdit: FileDiff,
  file: File,
  fileText: FileText,
  fileCode: FileCode,
  image: Image,
  /** ADR-0038: the composer's Attach button. */
  attach: Paperclip,
  mention: AtSign,
  pullRequest: GitPullRequest,
  /** G7: push / publish the task branch to origin. */
  push: Upload,
  /** G7: update the task branch from its base. */
  merge: GitMerge,
  issue: CircleDot,
  folder: Folder,
  folderOpen: FolderOpen,
  folderAdd: FolderPlus,
  workspace: FolderGit2,
  space: Layers,
  /** A collapsed run of tool calls. */
  toolGroup: Boxes,
  tool: Wrench,
  branch: GitBranch,
  /** ADR-0039 §8: Settings -> GitHub (lucide ships no brand glyphs). */
  forge: GitFork,
  /** O1 provenance: a run's worktree commit. */
  commit: GitCommitHorizontal,
  groupByStatus: ListChecks,
  groupByWorkspace: FolderGit2,
  groupByStage: Kanban,
  web: Globe,
  link: Link2,

  // --- agents / models
  agent: Bot,
  /** A provider with no brand mark of its own (a providers.custom ACP agent, an id the catalog has not described). */
  providerGeneric: Blocks,
  model: Cpu,
  thinking: Brain,
  /** The default-agent marker. */
  defaultAgent: Star,
  permission: ShieldCheck,
  permissionDanger: ShieldAlert,
  credentials: KeyRound,
  mcp: Plug,
  /** A plugin (Settings -> Plugins) and the "from plugin" owner badge. */
  plugin: Puzzle,
  /** Automations (Settings -> Automations): rules that start a task from a signal. */
  automation: CalendarClock,
  /** The Human inbox (ADR-0045): what needs a person, and its three kinds of row. */
  inbox: Inbox,
  verdict: ClipboardCheck,
  /** Learned skills (Settings -> Skills (learned)). */
  skills: BookOpen,

  // --- usage
  /** Account / plan usage windows. */
  usage: Gauge,
  time: Clock,

  // --- settings sections / hosts
  host: Server,
  connections: Network,
  notifications: Bell,
  appearance: Palette,
  themeLight: Sun,
  themeDark: Moon,
  themeSystem: Monitor,

  // --- states
  info: Info,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleAlert,
  /** Add `animate-spin` (and `motion-reduce:animate-none`). */
  spinner: LoaderCircle,
  /** An AI-assisted action (asking the agent to draft a commit message). */
  draft: Sparkles,
  /** A neutral / unfilled status marker. */
  statusIdle: Circle,
  /** A skipped / neutral / cancelled check: nothing ran, nothing failed. */
  statusSkipped: CircleMinus,
  /** A task tree that has sat idle past the stall alert threshold (no run, nothing queued). */
  stalled: Hourglass,
} as const satisfies Record<string, IconComponent>;

export type IconName = keyof typeof Icons;

/**
 * Size tokens by context (Tailwind needs the literal class strings). Pick by
 * *where* the icon sits, not by taste:
 *
 *  - `xs` (12px): inline with small text -- status/usage chips, timeline row
 *    markers and their disclosure chevrons, tab close/split affordances,
 *    `icon-xs` buttons.
 *  - `sm` (14px): dense controls -- tab-strip kind icons, file-tree rows,
 *    settings section headers, status rows, `sm` buttons, composer pills
 *    (md breakpoint and up).
 *  - `md` (16px): primary chrome -- sidebar rows and footer, header buttons,
 *    every menu item (dropdown, context, select), picker rows, dialog and
 *    alert icons, `icon-sm` buttons and the composer pills below md.
 *
 * `Button` already applies the matching size to a bare svg child per button
 * size, so only pass these when the container does not (or to override).
 * Colour: leading concept icons `text-foreground-muted`; trailing
 * affordances (chevrons) `text-foreground-subtle`.
 */
export const ICON_SIZE = {
  xs: "size-3",
  sm: "size-3.5",
  md: "size-4",
} as const;

export type IconSize = keyof typeof ICON_SIZE;
