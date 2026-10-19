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
// components/composer/provider-glyph.tsx, file-type glyphs in lib/file-icons.tsx.
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Archive,
  Bell,
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
  CircleQuestionMark,
  ClipboardPaste,
  Clock,
  Code,
  Coins,
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
  GitCommitHorizontal,
  GitCompare,
  Globe,
  Link2,
  Image,
  Info,
  KeyRound,
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
  PanelLeft,
  PanelRight,
  Pencil,
  Pin,
  PinOff,
  Plug,
  Plus,
  RefreshCw,
  RotateCcw,
  Rows2,
  Search,
  Server,
  Settings,
  ShieldAlert,
  ShieldCheck,
  Sigma,
  SlidersHorizontal,
  Square,
  Star,
  Sun,
  Terminal,
  Trash2,
  TriangleAlert,
  Undo2,
  Wrench,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";

export type IconComponent = LucideIcon;

export const Icons = {
  // --- actions
  add: Plus,
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
  folder: Folder,
  folderOpen: FolderOpen,
  folderAdd: FolderPlus,
  workspace: FolderGit2,
  space: Layers,
  /** A collapsed run of tool calls. */
  toolGroup: Boxes,
  tool: Wrench,
  branch: GitBranch,
  /** O1 provenance: a run's worktree commit. */
  commit: GitCommitHorizontal,
  groupByStatus: ListChecks,
  groupByWorkspace: FolderGit2,
  web: Globe,
  link: Link2,

  // --- agents / models
  agent: Bot,
  model: Cpu,
  thinking: Brain,
  /** The default-agent marker. */
  defaultAgent: Star,
  permission: ShieldCheck,
  permissionDanger: ShieldAlert,
  credentials: KeyRound,
  mcp: Plug,
  /** Learned skills (Settings -> Skills (learned)). */
  skills: BookOpen,

  // --- usage
  /** Account / plan usage windows. */
  usage: Gauge,
  usageTotal: Sigma,
  tokens: Coins,
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
  /** A neutral / unfilled status marker. */
  statusIdle: Circle,
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
