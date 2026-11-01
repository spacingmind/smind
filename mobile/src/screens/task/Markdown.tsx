// Markdown renders parseMarkdown's blocks (transcript/markdown.ts: the
// supported subset only) with nested selectable Text (expo-native-ui:
// selectable on transcript text). Links open through Linking.openURL; the
// parser already guarantees only http/https/mailto URLs become links.
// MarkdownBlocks is the same rendering as a reusable primitive: a caller
// that already holds parsed blocks (the plugin view renderer) passes an
// onLink to route safe links through its own action callback instead.

import { memo, ReactNode, useMemo } from 'react';
import { Linking, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Block, Inline, parseMarkdown } from '../../transcript/markdown';

function openLink(url: string) {
  void Linking.openURL(url).catch(() => {}); // an unhandled scheme must not crash the transcript
}

type LinkHandler = (url: string) => void;

function renderInlines(inlines: Inline[], styles: ReturnType<typeof makeStyles>, onLink?: LinkHandler): ReactNode[] {
  return inlines.map((inl, i) => {
    switch (inl.type) {
      case 'text':
        return inl.text;
      case 'bold':
        return (
          <Text key={i} style={styles.bold}>
            {renderInlines(inl.children, styles)}
          </Text>
        );
      case 'italic':
        return (
          <Text key={i} style={styles.italic}>
            {renderInlines(inl.children, styles)}
          </Text>
        );
      case 'code':
        return (
          <Text key={i} style={styles.inlineCode}>
            {inl.text}
          </Text>
        );
      case 'link':
        return (
          <Text key={i} style={styles.link} onPress={() => (onLink ?? openLink)(inl.url)} accessibilityRole="link">
            {inl.text}
          </Text>
        );
    }
  });
}

function BlockView({ block, styles, onLink }: { block: Block; styles: ReturnType<typeof makeStyles>; onLink?: LinkHandler }) {
  switch (block.type) {
    case 'paragraph':
      return (
        <Text style={styles.body} selectable>
          {renderInlines(block.inlines, styles, onLink)}
        </Text>
      );
    case 'heading':
      return (
        <Text style={block.level === 1 ? styles.h1 : block.level === 2 ? styles.h2 : styles.h3} selectable>
          {renderInlines(block.inlines, styles, onLink)}
        </Text>
      );
    case 'list':
      return (
        <View style={styles.list}>
          {block.items.map((item, i) => (
            <View key={i} style={styles.listItem}>
              <Text style={styles.bullet}>{block.ordered ? `${i + 1}.` : '•'}</Text>
              <Text style={styles.listText} selectable>
                {renderInlines(item, styles, onLink)}
              </Text>
            </View>
          ))}
        </View>
      );
    case 'code':
      return (
        <View style={styles.codeBlock}>
          <Text style={styles.codeText} selectable>
            {block.text}
          </Text>
        </View>
      );
    case 'quote':
      return (
        <View style={styles.quote}>
          <Text style={styles.quoteText} selectable>
            {renderInlines(block.inlines, styles, onLink)}
          </Text>
        </View>
      );
  }
}

export function MarkdownBlocks({ blocks, onLink }: { blocks: Block[]; onLink?: LinkHandler }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.root}>
      {blocks.map((block, i) => (
        <BlockView key={i} block={block} styles={styles} onLink={onLink} />
      ))}
    </View>
  );
}

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const blocks = useMemo(() => parseMarkdown(text), [text]);
  return <MarkdownBlocks blocks={blocks} />;
});

function makeStyles(theme: AppTheme) {
  const body = { ...theme.type.reading, color: theme.foreground };
  return StyleSheet.create({
    root: { gap: theme.spacing[3] },
    body,
    h1: { ...theme.type.heading1, color: theme.foreground },
    h2: { ...theme.type.heading2, color: theme.foreground },
    h3: { ...theme.type.label, color: theme.foreground },
    bold: { fontWeight: '600' },
    italic: { fontStyle: 'italic' },
    inlineCode: { fontFamily: theme.type.mono.fontFamily, fontSize: theme.type.mono.fontSize, backgroundColor: theme.surfaceSubtle },
    link: { color: theme.foreground, textDecorationLine: 'underline' },
    list: { gap: theme.spacing[1] },
    listItem: { flexDirection: 'row', gap: theme.spacing[2] },
    bullet: { ...body, minWidth: 16 },
    listText: { ...body, flex: 1 },
    codeBlock: {
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    codeText: { ...theme.type.mono, color: theme.foreground },
    quote: { borderLeftWidth: 2, borderLeftColor: theme.border, paddingLeft: theme.spacing[3] },
    quoteText: { ...body, color: theme.foregroundSubtle },
  });
}
