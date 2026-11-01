// PluginView.tsx is the React Native renderer for the pure plugin view
// model (view/model.ts, the web renderer's behaviour twin): it draws an
// already-built ViewNode tree through existing components and theme tokens
// only. Every untrusted string arrives already coerced by the model and is
// drawn as a <Text> child -- never HTML, never an image, never a fetched
// URL. renderNode wraps EVERY node in a NodeBoundary so one bad node
// degrades to the placeholder instead of blanking the panel.

import { ReactNode, useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, Text, View } from 'react-native';
import { Picker, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { MarkdownBlocks } from '../../screens/task/Markdown';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Badge, BadgeTone } from '../../ui/Badge';
import { Card } from '../../ui/Card';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';
import { buildView } from './model';
import { mdLinkAction } from './markdown';
import { NodeBoundary, UnsupportedNode } from './NodeBoundary';
import type { FormField, IconRef, Tone, ViewAction, ViewNode } from './types';

export type OnAction = (action: ViewAction, extra?: { formValues?: Record<string, string | boolean> }) => void;

function renderNode(node: ViewNode, onAction: OnAction): ReactNode {
  return (
    <NodeBoundary key={node.path}>
      <PluginNode node={node} onAction={onAction} />
    </NodeBoundary>
  );
}

export function PluginView({ view, onAction }: { view: ReturnType<typeof buildView>; onAction: OnAction }) {
  if (!view.supported) return <UnsupportedNode />;
  return renderNode(view.root, onAction);
}

function toneColor(theme: AppTheme, tone: Tone | undefined): { color: string } | undefined {
  switch (tone) {
    case 'success':
      return { color: theme.status.success };
    case 'warning':
      return { color: theme.status.warning };
    case 'danger':
      return { color: theme.status.danger };
    case 'muted':
      return { color: theme.foregroundSubtle };
    default:
      return undefined;
  }
}

function badgeTone(tone: Tone | undefined): BadgeTone {
  if (tone === 'success') return 'success';
  if (tone === 'warning') return 'warning';
  if (tone === 'danger') return 'destructive';
  return 'neutral';
}

/** A plugin icon ref as an AppIcon: named registry icons as-is, generic degrades to 'info', none renders nothing. */
function iconElement(icon: IconRef, size: number): ReactNode {
  if (icon.kind === 'name') return <AppIcon name={icon.name} size={size} />;
  if (icon.kind === 'generic') return <AppIcon name="info" size={size} />;
  return null;
}

function diffColor(theme: AppTheme, kind: 'add' | 'del' | 'hunk' | 'ctx'): string {
  if (kind === 'add') return theme.diff.addition;
  if (kind === 'del') return theme.diff.deletion;
  if (kind === 'hunk') return theme.foregroundSubtle;
  return theme.foreground;
}

function progressColor(theme: AppTheme, tone: Tone | undefined): string {
  const tinted = toneColor(theme, tone);
  return tinted === undefined ? theme.primary : tinted.color;
}

function PluginNode({ node, onAction }: { node: ViewNode; onAction: OnAction }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);

  switch (node.kind) {
    case 'stack': {
      const gap = node.gap === 'sm' ? theme.spacing[1] : node.gap === 'lg' ? theme.spacing[4] : theme.spacing[2];
      return (
        <View style={[node.row ? styles.rowStack : styles.colStack, { gap }, node.row && node.wrap ? styles.wrap : null]}>
          {node.children.map((child) => renderNode(child, onAction))}
        </View>
      );
    }

    case 'heading':
      return (
        <Text style={node.level === 1 ? styles.heading1 : node.level === 2 ? styles.heading2 : styles.headingLabel}>{node.text}</Text>
      );

    case 'text':
      return <Text style={[node.small ? styles.small : styles.body, toneColor(theme, node.tone)]}>{node.text}</Text>;

    case 'markdown':
      return (
        <MarkdownBlocks
          blocks={node.blocks}
          onLink={(url) => {
            const action = mdLinkAction(url);
            if (action !== null) onAction(action);
          }}
        />
      );

    case 'badge':
      return (
        <View style={styles.badgeRow}>
          {iconElement(node.icon, 16)}
          <Badge tone={badgeTone(node.tone)}>{node.text}</Badge>
        </View>
      );

    case 'button': {
      const action = node.action;
      return (
        <TextButton
          label={node.label}
          onPress={() => {
            if (action !== null) onAction(action);
          }}
          tone={node.tone === 'danger' ? 'danger' : 'default'}
          disabled={node.disabled || action === null}
        />
      );
    }

    case 'list':
      return (
        <View style={styles.listCol}>
          {node.items.map((item, i) => (
            <ListRow key={i} item={item} onAction={onAction} />
          ))}
        </View>
      );

    case 'keyValue':
      return (
        <View style={styles.kvCol}>
          {node.items.map((item, i) => (
            <View key={i} style={styles.kvRow}>
              <Text style={styles.kvKey}>{item.key}</Text>
              <Text style={[styles.kvValue, toneColor(theme, item.tone)]}>{item.value}</Text>
            </View>
          ))}
        </View>
      );

    case 'table':
      return (
        <ScrollView horizontal>
          <View>
            <View style={styles.tableRow}>
              {node.columns.map((column, i) => (
                <Text key={i} style={[styles.tableHeader, column.alignEnd ? styles.alignEnd : null]}>
                  {column.label}
                </Text>
              ))}
            </View>
            {node.rows.map((row, i) => (
              <View key={i} style={styles.tableRow}>
                {row.map((cell, j) => (
                  <Text key={j} style={[styles.tableCell, node.columns[j]?.alignEnd ? styles.alignEnd : null]}>
                    {cell}
                  </Text>
                ))}
              </View>
            ))}
          </View>
        </ScrollView>
      );

    case 'code':
      return (
        <View style={styles.codeBox}>
          <ScrollView horizontal>
            <Text style={styles.monoText} selectable>
              {node.text}
            </Text>
          </ScrollView>
        </View>
      );

    case 'diff':
      return (
        <View style={styles.codeBox}>
          <ScrollView horizontal>
            <View>
              {node.lines.map((line, i) => (
                <Text key={i} style={[styles.monoText, { color: diffColor(theme, line.kind) }]}>
                  {line.text}
                </Text>
              ))}
            </View>
          </ScrollView>
        </View>
      );

    case 'progress': {
      const color = progressColor(theme, node.tone);
      return (
        <View style={styles.progressCol}>
          {node.label !== '' ? <Text style={styles.progressLabel}>{node.label}</Text> : null}
          <View
            style={styles.progressTrack}
            accessibilityRole="progressbar"
            accessibilityValue={{ min: 0, max: 100, now: node.pct }}
          >
            <View style={[styles.progressFill, { width: `${node.pct}%`, backgroundColor: color }]} />
          </View>
        </View>
      );
    }

    case 'form':
      return <FormNode node={node} onAction={onAction} />;

    case 'empty':
      return (
        <View style={styles.emptyCol}>
          {iconElement(node.icon, 20)}
          <Text style={styles.emptyTitle}>{node.title}</Text>
          {node.text !== '' ? <Text style={styles.emptyText}>{node.text}</Text> : null}
        </View>
      );

    case 'error':
      return (
        <Card style={styles.errorCard}>
          <Text style={styles.errorTitle}>{node.title}</Text>
          <Text style={styles.errorText}>{node.text}</Text>
        </Card>
      );

    case 'unsupported':
      return <UnsupportedNode nodeType={node.nodeType} />;
  }
}

type ListItem = Extract<ViewNode, { kind: 'list' }>['items'][number];

function ListRow({ item, onAction }: { item: ListItem; onAction: OnAction }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const action = item.action;
  const content = (
    <>
      <Text style={[styles.listTitle, toneColor(theme, item.tone)]} numberOfLines={1}>
        {item.title}
      </Text>
      {item.subtitle !== '' ? (
        <Text style={styles.listSubtitle} numberOfLines={1}>
          {item.subtitle}
        </Text>
      ) : null}
      {item.badge !== '' ? <Badge>{item.badge}</Badge> : null}
      {action !== null ? <AppIcon name="chevronRight" /> : null}
    </>
  );
  if (action === null) return <View style={styles.listRow}>{content}</View>;
  return (
    <Pressable style={styles.listRow} accessibilityRole="button" accessibilityLabel={item.title} onPress={() => onAction(action)}>
      {content}
    </Pressable>
  );
}

function FormNode({ node, onAction }: { node: Extract<ViewNode, { kind: 'form' }>; onAction: OnAction }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [values, setValues] = useState<Record<string, string | boolean>>(() => {
    const init: Record<string, string | boolean> = {};
    for (const field of node.fields) init[field.name] = field.default;
    return init;
  });
  const setValue = (name: string, value: string | boolean) => setValues((prev) => ({ ...prev, [name]: value }));
  // A required field blocks submit until it has a value; a toggle never blocks.
  const blocked = node.fields.some((field) => field.required && field.type !== 'toggle' && !values[field.name]);
  const action = node.action;
  const submit = () => {
    if (action === null || blocked) return;
    onAction(action, { formValues: values });
  };
  return (
    <View style={styles.formCol}>
      {node.fields.map((field) => (
        <FormFieldRow key={field.name} field={field} value={values[field.name]} onValue={setValue} />
      ))}
      <TextButton label={node.submitLabel} onPress={submit} disabled={blocked || action === null} />
    </View>
  );
}

function FormFieldRow({
  field,
  value,
  onValue,
}: {
  field: FormField;
  value: string | boolean;
  onValue: (name: string, value: string | boolean) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  // @expo/ui's TextInput binds text via ObservableState (NewTaskScreen's note); React state stays the source of truth.
  const text = useNativeState(typeof value === 'string' ? value : '');
  let control: ReactNode;
  if (field.type === 'toggle') {
    control = (
      <Switch
        value={value === true}
        onValueChange={(b) => onValue(field.name, b)}
        trackColor={{ false: theme.secondary, true: theme.primary }}
        thumbColor={theme.card}
        ios_backgroundColor={theme.secondary}
      />
    );
  } else if (field.type === 'select') {
    control = (
      <UiHost style={styles.controlHost}>
        <Picker selectedValue={typeof value === 'string' ? value : ''} onValueChange={(v) => onValue(field.name, String(v))}>
          {field.options.map((option) => (
            <Picker.Item key={option.value} label={option.label} value={option.value} />
          ))}
        </Picker>
      </UiHost>
    );
  } else {
    control = (
      <UiHost style={styles.controlHost}>
        <ExpoTextInput
          style={styles.inputBox}
          textStyle={styles.inputText}
          placeholder={field.placeholder}
          placeholderTextColor={theme.foregroundSubtlest}
          value={text}
          secureTextEntry={field.type === 'secret'}
          onChangeText={(t) => {
            text.value = t;
            onValue(field.name, t);
          }}
        />
      </UiHost>
    );
  }
  return (
    <View style={styles.fieldCol}>
      <Text style={styles.fieldLabel}>
        {field.label}
        {field.required ? ' *' : null}
      </Text>
      {control}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    colStack: { flexDirection: 'column' },
    rowStack: { flexDirection: 'row', alignItems: 'center' },
    wrap: { flexWrap: 'wrap' },
    heading1: { ...theme.type.heading1, color: theme.foreground },
    heading2: { ...theme.type.heading2, color: theme.foreground },
    headingLabel: { ...theme.type.label, color: theme.foreground },
    body: { ...theme.type.body, color: theme.foreground },
    small: { ...theme.type.small, color: theme.foreground },
    badgeRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1] },
    listCol: { gap: theme.spacing[1] },
    listRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2], paddingVertical: theme.spacing[1] },
    listTitle: { ...theme.type.body, color: theme.foreground, flexShrink: 1 },
    listSubtitle: { ...theme.type.small, color: theme.foregroundSubtle },
    kvCol: { gap: theme.spacing[1] },
    kvRow: { flexDirection: 'row', gap: theme.spacing[3] },
    kvKey: { ...theme.type.small, color: theme.foregroundSubtle },
    kvValue: { ...theme.type.body, color: theme.foreground, flex: 1 },
    tableRow: { flexDirection: 'row' },
    tableHeader: {
      ...theme.type.label,
      color: theme.foreground,
      width: 112,
      paddingVertical: theme.spacing[1],
      paddingRight: theme.spacing[2],
    },
    tableCell: {
      ...theme.type.small,
      color: theme.foreground,
      width: 112,
      paddingVertical: theme.spacing[1],
      paddingRight: theme.spacing[2],
    },
    alignEnd: { textAlign: 'right' },
    codeBox: {
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[2],
    },
    monoText: { ...theme.type.mono, color: theme.foreground },
    progressCol: { gap: theme.spacing[1] },
    progressLabel: { ...theme.type.small, color: theme.foregroundSubtle },
    progressTrack: { height: 6, borderRadius: theme.radius.full, backgroundColor: theme.surfaceSubtle, overflow: 'hidden' },
    progressFill: { height: 6, borderRadius: theme.radius.full },
    formCol: { gap: theme.spacing[3] },
    fieldCol: { gap: theme.spacing[1] },
    fieldLabel: { ...theme.type.small, color: theme.foregroundSubtle },
    controlHost: { alignSelf: 'stretch' },
    inputBox: {
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    inputText: {
      // Web: 16px keeps iOS Safari from zooming on focus (expo-native-ui input floor).
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize,
      color: theme.foreground,
    },
    emptyCol: { alignItems: 'center', gap: theme.spacing[2], paddingVertical: theme.spacing[6] },
    emptyTitle: { ...theme.type.label, color: theme.foreground, textAlign: 'center' },
    emptyText: { ...theme.type.caption, color: theme.foregroundSubtle, textAlign: 'center' },
    errorCard: { gap: theme.spacing[1] },
    errorTitle: { ...theme.type.label, color: theme.status.danger },
    errorText: { ...theme.type.body, color: theme.foreground },
  });
}
