// ForkContext: what a forked chat inherited, read-only and de-emphasised
// above its own turns (ADR-0057 PR 1b AC21): a native fork's parent turns
// up to the fork point (collapsed to "<n> earlier turns", loaded by the
// screen on expand only), or a seeded fork's summary. Either way the
// "Forked here" divider (chatFork.ts's forkDividerLabel) marks where this
// chat starts. The only action here is the expand toggle; the wording and
// slicing live in chatFork.ts.

import { memo, useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { earlierTurnsLabel } from '../../chatFork';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';

/** One inherited turn as the screen hands it over: the prompt and the assistant's final text (null when the turn has none). */
export interface ForkTurn {
  key: string;
  prompt: string;
  answer: string | null;
}

export const ForkContext = memo(function ForkContext({
  dividerLabel,
  loading,
  mode,
  parentTitle,
  turns,
  turnCount,
  expanded,
  onToggle,
  seed,
  seedFailed,
}: {
  dividerLabel: string;
  loading: boolean;
  mode: 'native' | 'seeded';
  parentTitle: string | null;
  turns: ForkTurn[] | null;
  turnCount: number;
  expanded: boolean;
  onToggle: () => void;
  seed: string | null;
  seedFailed: boolean;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const header = mode === 'native' ? earlierTurnsLabel(turnCount) : 'Summary sent to the agent';
  return (
    <View style={styles.block} accessibilityLabel={mode === 'native' ? (parentTitle !== null ? `Earlier turns from ${parentTitle}` : 'Earlier turns') : 'Summary sent to the agent'}>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded }}
        accessibilityLabel={header}
        onPress={onToggle}
        style={({ pressed }) => [styles.header, pressed && styles.pressed]}
      >
        <View style={[styles.chevron, expanded && styles.chevronOpen]}>
          <AppIcon name="chevronRight" size={theme.type.small.fontSize} color={theme.foregroundSubtle} />
        </View>
        <Text style={styles.headerText}>{header}</Text>
      </Pressable>
      {expanded ? (
        mode === 'native' ? (
          loading ? (
            <Text style={styles.loading}>Loading earlier turns…</Text>
          ) : (
            (turns ?? []).map((turn) => (
              <View key={turn.key} style={styles.turn}>
                <Text style={styles.prompt} selectable>
                  {turn.prompt}
                </Text>
                {turn.answer !== null ? (
                  <Text style={styles.answer} selectable>
                    {turn.answer}
                  </Text>
                ) : null}
              </View>
            ))
          )
        ) : (
          <View style={styles.seedBody}>
            <Text style={styles.seedNote} selectable>
              The agent was given this account of the earlier turns instead of its own memory of them; it does not remember tool calls or reasoning.
            </Text>
            {seedFailed ? (
              <Text style={styles.seedText} selectable>
                The summary could not be loaded.
              </Text>
            ) : (
              <Text style={styles.seedText} selectable>
                {seed ?? ''}
              </Text>
            )}
          </View>
        )
      ) : null}
      <View style={styles.divider}>
        <View style={styles.rule} />
        <Text style={styles.dividerText}>{dividerLabel}</Text>
        <View style={styles.rule} />
      </View>
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    // Read-only inherited content: dimmed (AC21's de-emphasis), never a hex literal.
    block: { gap: theme.spacing[3], opacity: 0.6 },
    header: { minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1], alignSelf: 'flex-start' },
    pressed: { opacity: 0.6 },
    chevron: { transform: [{ rotate: '0deg' }] },
    chevronOpen: { transform: [{ rotate: '90deg' }] },
    headerText: { ...theme.type.small, color: theme.foregroundSubtle },
    loading: { ...theme.type.small, color: theme.foregroundSubtle },
    turn: { gap: theme.spacing[1] },
    prompt: { ...theme.type.reading, color: theme.foreground },
    answer: { ...theme.type.small, color: theme.foregroundSubtle },
    seedBody: { gap: theme.spacing[2] },
    seedNote: { ...theme.type.small, color: theme.foregroundSubtle },
    seedText: { ...theme.type.reading, color: theme.foreground },
    divider: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[3], paddingVertical: theme.spacing[1] },
    rule: { flex: 1, height: StyleSheet.hairlineWidth, backgroundColor: theme.border },
    dividerText: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
