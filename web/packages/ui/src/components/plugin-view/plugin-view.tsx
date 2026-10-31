// The web renderer for daemon-validated ui:1 plugin view documents
// (internal/plugins/uiview/schema.go). Purely presentational: it takes an
// already-fetched document and an onAction callback and knows nothing about
// queries, panels or tabs. Every value from the document is rendered as a
// React text child -- never as HTML -- so untrusted plugin data can only
// become text, theme-token classes and allowlisted svg geometry.
import { Component, Fragment, useState, type FormEvent, type ReactNode } from "react";
import type { Components } from "react-markdown";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";
import { SafeSvg } from "@/components/plugin-view/safe-svg";
import { Icons, pluginIconByName, type IconComponent } from "@/lib/icons";
import type { PluginActionExtra, PluginTone, PluginViewAction } from "@/lib/plugin-view-types";
import { cn } from "@/lib/utils";

type OnAction = (action: PluginViewAction, extra?: PluginActionExtra) => void;

const SUPPORTED_UI_LEVEL = 1;
const MAX_RENDER_DEPTH = 16;

const NODE_TYPES = new Set([
  "badge", "button", "code", "diff", "empty", "error", "form", "heading",
  "keyValue", "list", "markdown", "progress", "row", "stack", "table", "text",
]);

const TONE_TEXT: Record<PluginTone, string> = {
  success: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
  muted: "text-foreground-muted",
};

const TONE_BG: Record<PluginTone, string> = {
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-destructive",
  muted: "bg-foreground-muted",
};

const GAP_CLASS: Record<string, string> = { sm: "gap-1.5", md: "gap-3", lg: "gap-5" };

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === "object" && x !== null && !Array.isArray(x);
}

function str(x: unknown): string {
  return typeof x === "string" ? x : "";
}

function toneText(tone: unknown): string | null {
  return typeof tone === "string" && tone in TONE_TEXT ? TONE_TEXT[tone as PluginTone] : null;
}

function toneBg(tone: unknown): string {
  return typeof tone === "string" && tone in TONE_BG ? TONE_BG[tone as PluginTone] : "";
}

function asAction(x: unknown): PluginViewAction | null {
  return isRecord(x) && typeof x.kind === "string" ? (x as PluginViewAction) : null;
}

/** The one fallback surface: a node this app version cannot render. */
function UnsupportedNode({ nodeType }: { nodeType?: string }) {
  return (
    <div
      data-testid="plugin-unsupported-node"
      data-node-type={nodeType}
      role="note"
      className="rounded-md border border-dashed border-border px-2 py-1.5 text-ui-sm text-foreground-muted"
    >
      Unsupported in this app version
    </div>
  );
}

/** One bad node never blanks the panel: it degrades to the placeholder. */
class PluginNodeBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    return this.state.failed ? <UnsupportedNode /> : this.props.children;
  }
}

function PluginIconGlyph({
  icon,
  className,
  defaultIcon,
}: {
  icon?: unknown;
  className?: string;
  defaultIcon?: IconComponent;
}) {
  if (typeof icon === "string") {
    const Cmp = pluginIconByName(icon);
    return <Cmp aria-hidden className={className} />;
  }
  if (isRecord(icon) && typeof icon.svg === "string") {
    return <SafeSvg svg={icon.svg} className={className} />;
  }
  if (defaultIcon) {
    const Cmp = defaultIcon;
    return <Cmp aria-hidden className={className} />;
  }
  return null;
}

function BadgeSpan({ text, tone }: { text: string; tone?: unknown }) {
  return <span className={cn("rounded-full border border-border px-2 py-0.5 text-ui-sm", toneText(tone))}>{text}</span>;
}

function cellText(cell: unknown): string {
  if (cell === null || cell === undefined) return "";
  if (cell === true) return "Yes";
  if (cell === false) return "No";
  if (typeof cell === "number") return String(cell);
  if (typeof cell === "string") return cell;
  return "";
}

function diffLineClass(line: string): string {
  if (line.startsWith("@@")) return "text-foreground-muted";
  if (line.startsWith("+") && !line.startsWith("+++")) return "text-success";
  if (line.startsWith("-") && !line.startsWith("---")) return "text-destructive";
  return "";
}

function MarkdownNode({ text, onAction }: { text: string; onAction: OnAction }) {
  // Raw HTML stays inert (no remark plugin is enabled to turn document
  // markup into elements). Images never render (the alt text stands in for
  // them) and http(s) links go through onAction as openUrl so the caller
  // can confirm the host.
  const components: Components = {
    img: ({ alt }) => <span>{typeof alt === "string" ? alt : ""}</span>,
    a: ({ href, children }) => {
      if (typeof href === "string" && /^https?:\/\//i.test(href)) {
        let host: string | null = null;
        try {
          host = new URL(href).host;
        } catch {
          host = null;
        }
        if (host) {
          return (
            <button
              type="button"
              className="underline"
              onClick={() => onAction({ kind: "openUrl", url: href, host })}
            >
              {children}
            </button>
          );
        }
      }
      return <>{children}</>;
    },
  };
  return <TimelineMarkdown content={text} components={components} data-testid="plugin-markdown" />;
}

function ListRow({ item, onAction }: { item: Record<string, unknown>; onAction: OnAction }) {
  const action = asAction(item.action);
  const subtitle = str(item.subtitle);
  const badge = str(item.badge);
  const content = (
    <>
      <PluginIconGlyph icon={item.icon} className="size-4 shrink-0 text-foreground-muted" />
      <span className={cn("min-w-0 flex-1 truncate text-left", toneText(item.tone))}>{str(item.title)}</span>
      {subtitle && <span className="shrink-0 text-ui-sm text-foreground-muted">{subtitle}</span>}
      {badge && <BadgeSpan text={badge} />}
    </>
  );
  if (action) {
    return (
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-ui-base hover:bg-muted"
        onClick={() => onAction(action)}
      >
        {content}
      </button>
    );
  }
  return <div className="flex w-full items-center gap-2 px-1.5 py-1 text-ui-base">{content}</div>;
}

function FormNode({ node, onAction }: { node: Record<string, unknown>; onAction: OnAction }) {
  const formId = typeof node.id === "string" && node.id ? node.id : "form";
  const fields = (Array.isArray(node.fields) ? node.fields.filter(isRecord) : []).filter(
    (f) => typeof f.name === "string" && f.name !== "",
  );
  const submit = isRecord(node.submit) ? node.submit : {};
  const action = asAction(submit.action);
  const [values, setValues] = useState<Record<string, string | boolean>>(() => {
    const init: Record<string, string | boolean> = {};
    for (const f of fields) {
      init[f.name as string] =
        f.type === "toggle" ? f.default === true : typeof f.default === "string" ? f.default : "";
    }
    return init;
  });
  const setValue = (name: string, value: string | boolean) => setValues((prev) => ({ ...prev, [name]: value }));
  // A required field blocks submit until it has a value; a toggle never blocks.
  const blocked = fields.some(
    (f) => f.required === true && f.type !== "toggle" && !values[f.name as string],
  );
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!action || blocked) return;
    onAction(action, { formValues: values });
  };

  return (
    <form data-testid={`plugin-form-${formId}`} className="flex flex-col gap-3" onSubmit={onSubmit}>
      {fields.map((f) => {
        const name = f.name as string;
        const fieldId = `plugin-form-${formId}-${name}`;
        const label = (
          <span className="flex items-baseline gap-1 text-ui-sm text-foreground-muted">
            <label htmlFor={fieldId}>{str(f.label)}</label>
            {f.required === true && <span aria-hidden>*</span>}
          </span>
        );
        let control: ReactNode;
        if (f.type === "toggle") {
          control = (
            <Switch id={fieldId} checked={values[name] === true} onCheckedChange={(b) => setValue(name, b)} />
          );
        } else if (f.type === "select") {
          const options = Array.isArray(f.options) ? f.options.filter(isRecord) : [];
          control = (
            <select
              id={fieldId}
              value={typeof values[name] === "string" ? (values[name] as string) : ""}
              onChange={(e) => setValue(name, e.target.value)}
              className="h-8 w-full min-w-0 rounded-lg border border-input-border bg-input px-2 text-ui-base outline-none"
            >
              {options.map((o, j) => (
                <option key={j} value={str(o.value)}>
                  {str(o.label)}
                </option>
              ))}
            </select>
          );
        } else {
          control = (
            <Input
              id={fieldId}
              type={f.type === "secret" ? "password" : "text"}
              autoComplete={f.type === "secret" ? "off" : undefined}
              value={typeof values[name] === "string" ? (values[name] as string) : ""}
              placeholder={str(f.placeholder)}
              onChange={(e) => setValue(name, e.target.value)}
            />
          );
        }
        return (
          <div key={name} className="flex flex-col gap-1">
            {label}
            {control}
          </div>
        );
      })}
      <div>
        <Button type="submit" size="sm" disabled={blocked}>
          {str(submit.label)}
        </Button>
      </div>
    </form>
  );
}

function PluginNode({ node, depth, path, onAction }: { node: unknown; depth: number; path: string; onAction: OnAction }) {
  if (!isRecord(node) || depth > MAX_RENDER_DEPTH) {
    return <UnsupportedNode nodeType={isRecord(node) && typeof node.type === "string" ? node.type : undefined} />;
  }
  const type = node.type;
  if (typeof type !== "string" || !NODE_TYPES.has(type)) {
    return <UnsupportedNode nodeType={typeof type === "string" ? type : undefined} />;
  }

  switch (type) {
    case "stack":
    case "row": {
      const children = Array.isArray(node.children) ? node.children : [];
      const gap = typeof node.gap === "string" && node.gap in GAP_CLASS ? GAP_CLASS[node.gap] : GAP_CLASS.md;
      return (
        <div
          className={cn(
            type === "stack" ? "flex flex-col" : "flex items-center",
            type === "row" && node.wrap === true && "flex-wrap",
            gap,
          )}
        >
          {children.map((child, i) => renderNode(child, depth + 1, `${path}.children[${i}]`, onAction))}
        </div>
      );
    }

    case "heading": {
      const level =
        node.level === 1 || node.level === 2 || node.level === 3 || node.level === 4 ? node.level : 2;
      const cls =
        level === 1
          ? "text-ui-xl font-medium"
          : level === 2
            ? "text-ui-lg font-medium"
            : "text-ui-base font-semibold";
      const Tag = (["h1", "h2", "h3", "h4"] as const)[level - 1];
      return <Tag className={cls}>{str(node.text)}</Tag>;
    }

    case "text":
      return (
        <p className={cn(node.size === "sm" ? "text-ui-sm" : "text-ui-base", toneText(node.tone))}>
          {str(node.text)}
        </p>
      );

    case "markdown":
      return <MarkdownNode text={str(node.text)} onAction={onAction} />;

    case "badge":
      return (
        <span
          className={cn(
            "inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-ui-sm",
            toneText(node.tone),
          )}
        >
          <PluginIconGlyph icon={node.icon} className="size-3.5 shrink-0" />
          {str(node.text)}
        </span>
      );

    case "button": {
      const action = asAction(node.action);
      return (
        <Button
          variant="outline"
          size="sm"
          disabled={node.disabled === true}
          className={cn(node.tone === "danger" && "text-destructive")}
          onClick={action ? () => onAction(action) : undefined}
        >
          <PluginIconGlyph icon={node.icon} className="size-3.5 shrink-0" />
          {str(node.label)}
        </Button>
      );
    }

    case "list": {
      const items = Array.isArray(node.items) ? node.items.filter(isRecord) : [];
      return (
        <ul className="flex flex-col">
          {items.map((item, i) => (
            <li key={i}>
              <ListRow item={item} onAction={onAction} />
            </li>
          ))}
        </ul>
      );
    }

    case "keyValue": {
      const items = Array.isArray(node.items) ? node.items.filter(isRecord) : [];
      return (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-ui-sm">
          {items.map((item, i) => (
            <Fragment key={i}>
              <dt className="text-foreground-muted">{str(item.key)}</dt>
              <dd className={cn(toneText(item.tone))}>{str(item.value)}</dd>
            </Fragment>
          ))}
        </dl>
      );
    }

    case "table": {
      const columns = (Array.isArray(node.columns) ? node.columns.filter(isRecord) : []).filter(
        (c) => typeof c.key === "string",
      );
      const rows = Array.isArray(node.rows) ? node.rows.filter(isRecord) : [];
      return (
        <div className="overflow-x-auto">
          <table className="w-full text-ui-sm">
            <thead>
              <tr>
                {columns.map((c, i) => (
                  <th
                    key={i}
                    className={cn("border-b border-border px-2 py-1 text-left font-medium", c.align === "end" && "text-right")}
                  >
                    {str(c.label)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, i) => (
                <tr key={i}>
                  {columns.map((c, j) => (
                    <td key={j} className="border-b border-border px-2 py-1">
                      {cellText(row[c.key as string])}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    }

    case "code":
      return (
        <div className="overflow-x-auto rounded-lg bg-surface p-2.5">
          <pre className="font-mono text-ui-sm">
            <code>{str(node.text)}</code>
          </pre>
        </div>
      );

    case "diff":
      return (
        <div className="overflow-x-auto rounded-lg bg-surface p-2.5 font-mono text-ui-sm">
          {str(node.text).split("\n").map((line, i) => (
            <div key={i} className={cn("whitespace-pre", diffLineClass(line))}>
              {line}
            </div>
          ))}
        </div>
      );

    case "progress": {
      const raw = node.value;
      const value = typeof raw === "number" && Number.isFinite(raw) ? raw : 0;
      const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
      return (
        <div className="flex flex-col gap-1">
          {str(node.label) && <span className="text-ui-sm text-foreground-muted">{str(node.label)}</span>}
          <div
            role="progressbar"
            aria-valuenow={pct}
            aria-valuemin={0}
            aria-valuemax={100}
            className="h-1.5 overflow-hidden rounded-full bg-surface"
          >
            <div
              className={cn("h-full rounded-full", toneBg(node.tone) || "bg-primary")}
              style={{ width: `${pct}%` }}
            />
          </div>
        </div>
      );
    }

    case "form":
      return <FormNode node={node} onAction={onAction} />;

    case "empty":
      return (
        <div className="flex flex-col items-center gap-2 py-6 text-center">
          <PluginIconGlyph icon={node.icon} defaultIcon={Icons.plugin} className="size-5 text-foreground-muted" />
          <div className="text-ui-base font-medium">{str(node.title)}</div>
          {str(node.text) && <p className="text-ui-sm text-foreground-muted">{str(node.text)}</p>}
        </div>
      );

    case "error":
      return <Alert variant="error" title={str(node.title)} description={str(node.text)} />;
  }
}

function renderNode(node: unknown, depth: number, path: string, onAction: OnAction): ReactNode {
  return (
    <PluginNodeBoundary key={path}>
      <PluginNode node={node} depth={depth} path={path} onAction={onAction} />
    </PluginNodeBoundary>
  );
}

export function PluginView({ doc, onAction }: { doc: unknown; onAction: OnAction }) {
  const record = isRecord(doc) ? doc : null;
  const level = record && typeof record.ui === "number" ? record.ui : null;
  const supported = record !== null && level !== null && level <= SUPPORTED_UI_LEVEL && "root" in record;
  return (
    <div data-testid="plugin-view">
      {supported ? renderNode(record.root, 0, "root", onAction) : <UnsupportedNode />}
    </div>
  );
}
