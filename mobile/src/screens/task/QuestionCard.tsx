// QuestionCard: a pending agent question inside the task transcript (ADR-0055), where the permission card sits.
// A form, one block per question: radio rows ("single"), checkbox rows ("multi") or a text field ("text"), each option
// with its description, an "Other" line when the question allows one, then a note field and Submit / Decline.
//
// Security (ADR-0055 section 7): every agent-authored string (header, question, option label and description) renders
// as plain <Text>: no markdown component, no Linking, no autolinking, so a URL in it is only characters. Nothing is
// ever preselected, there is no "recommended" emphasis, and Submit stays disabled, with the reason written under the
// form, until every question has an answer. State is the pure questionAnswer.ts; this file only lays it out.

import { memo, useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { Button } from '@expo/ui';
import {
  Draft,
  MAX_NOTE_CHARS,
  Question,
  QuestionAnswer,
  declinePayload,
  draftOf,
  setFreeText,
  setOtherText,
  submitBlockedReason,
  submitPayload,
  toggleOption,
} from '../../questionAnswer';
import { clampRunes } from '../../permissionRequests';
import type { QuestionState } from '../../questionRequests';
import { COLLAPSED_LINES, isLongAsk } from '../../questionView';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Card } from '../../ui/Card';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';

export const QuestionCard = memo(function QuestionCard({
  state,
  onAnswer,
}: {
  state: QuestionState;
  onAnswer: (requestId: string, answer: QuestionAnswer) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [note, setNote] = useState('');
  const questions = state.request.questions;
  const sending = state.status === 'resolved'; // this device's answer is in flight; an event (not a tap) closes the card
  const blocked = sending ? 'Sending your answer…' : submitBlockedReason(questions, drafts);

  return (
    <Card style={styles.card}>
      <View style={styles.badge} accessibilityRole="text">
        <Text style={styles.badgeText}>Waiting for your response</Text>
      </View>
      {state.request.truncated ? <Text style={styles.muted}>Some of this text was shortened.</Text> : null}
      {questions.map((q) => (
        <QuestionField
          key={q.id}
          question={q}
          draft={draftOf(drafts, q.id)}
          disabled={sending}
          onChange={(next) => setDrafts((prev) => ({ ...prev, [q.id]: next }))}
        />
      ))}
      <View style={styles.noteField}>
        <Text style={styles.label}>Note for the agent (optional)</Text>
        <TextInput
          style={styles.input}
          value={note}
          onChangeText={(next) => setNote(clampRunes(next, MAX_NOTE_CHARS))}
          placeholder="Sent only if you decline"
          placeholderTextColor={theme.foregroundSubtlest}
          accessibilityLabel="Note for the agent (optional)"
        />
      </View>
      {blocked !== null ? (
        <Text style={styles.muted} accessibilityLiveRegion="polite">
          {blocked}
        </Text>
      ) : null}
      <View style={styles.actions}>
        <UiHost>
          <Button
            label="Submit"
            variant="filled"
            disabled={blocked !== null}
            onPress={() => {
              const payload = sending ? null : submitPayload(questions, drafts);
              if (payload) onAnswer(state.requestId, payload);
            }}
            style={styles.button}
          />
        </UiHost>
        <TextButton label="Decline" disabled={sending} onPress={() => onAnswer(state.requestId, declinePayload(note))} />
      </View>
      {state.error !== null ? (
        <Text style={styles.error} selectable>
          Couldn't send the answer: {state.error}
        </Text>
      ) : null}
    </Card>
  );
});

function QuestionField({ question, draft, disabled, onChange }: { question: Question; draft: Draft; disabled: boolean; onChange: (next: Draft) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [expanded, setExpanded] = useState(false);
  const long = isLongAsk(question.text);
  const isChoice = question.kind === 'single' || question.kind === 'multi';
  return (
    <View style={styles.field} accessibilityRole="none">
      {question.header ? (
        <View style={styles.chip}>
          <Text style={styles.chipText}>{question.header}</Text>
        </View>
      ) : null}
      <Text style={styles.question} selectable numberOfLines={long && !expanded ? COLLAPSED_LINES : undefined}>
        {question.text}
      </Text>
      {long ? (
        <Pressable accessibilityRole="button" accessibilityState={{ expanded }} onPress={() => setExpanded((v) => !v)} style={styles.toggle}>
          <Text style={styles.toggleText}>{expanded ? 'Show less' : 'Show all'}</Text>
        </Pressable>
      ) : null}
      {isChoice
        ? (question.options ?? []).map((option) => {
            const selected = draft.optionIds.includes(option.id);
            return (
              <Pressable
                key={option.id}
                accessibilityRole={question.kind === 'multi' ? 'checkbox' : 'radio'}
                accessibilityState={{ checked: selected }}
                accessibilityLabel={option.label}
                disabled={disabled}
                onPress={() => onChange(toggleOption(question, draft, option.id))}
                style={({ pressed }) => [styles.option, selected && styles.optionSelected, pressed && styles.pressed]}
              >
                <View style={[question.kind === 'multi' ? styles.box : styles.radio, selected && styles.markOn]}>
                  {selected ? <AppIcon name="check" size={12} color={theme.primaryForeground} /> : null}
                </View>
                <View style={styles.optionText}>
                  <Text style={styles.optionLabel} selectable>
                    {option.label}
                  </Text>
                  {option.description ? (
                    <Text style={styles.muted} selectable>
                      {option.description}
                    </Text>
                  ) : null}
                </View>
              </Pressable>
            );
          })
        : null}
      {question.kind === 'text' ? (
        <TextInput
          style={[styles.input, styles.multiline]}
          value={draft.text}
          onChangeText={(next) => onChange(setFreeText(draft, next))}
          placeholder="Type your answer…"
          placeholderTextColor={theme.foregroundSubtlest}
          multiline
          editable={!disabled}
          accessibilityLabel={`Answer: ${question.text.slice(0, 80)}`}
        />
      ) : question.allowOther ? (
        <View style={styles.otherField}>
          <Text style={styles.label}>Other:</Text>
          <TextInput
            style={styles.input}
            value={draft.otherText}
            onChangeText={(next) => onChange(setOtherText(question, draft, next))}
            editable={!disabled}
            placeholder="Type another answer…"
            placeholderTextColor={theme.foregroundSubtlest}
            accessibilityLabel={`Other answer: ${question.text.slice(0, 80)}`}
          />
        </View>
      ) : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  const mark = { width: 20, height: 20, borderWidth: 1.5, borderColor: theme.foregroundSubtle, alignItems: 'center', justifyContent: 'center', marginTop: 1 } as const;
  return StyleSheet.create({
    card: { backgroundColor: theme.waiting.surface, gap: theme.spacing[3] },
    badge: { alignSelf: 'flex-start', borderRadius: theme.radius.full, paddingHorizontal: theme.spacing[2], paddingVertical: 2, backgroundColor: theme.card },
    badgeText: { ...theme.type.small, color: theme.waiting.foreground, fontWeight: '600' },
    field: { gap: theme.spacing[2] },
    chip: { alignSelf: 'flex-start', borderRadius: theme.radius.full, borderWidth: 1, borderColor: theme.border, paddingHorizontal: theme.spacing[2], paddingVertical: 2 },
    chipText: { ...theme.type.small, color: theme.foregroundSubtle },
    question: { ...theme.type.body, color: theme.foreground },
    toggle: { alignSelf: 'flex-start', minHeight: 44, justifyContent: 'center' },
    toggleText: { ...theme.type.label, color: theme.foreground },
    option: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: theme.spacing[3],
      minHeight: 44,
      padding: theme.spacing[3],
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      borderWidth: 1,
      borderColor: theme.border,
      backgroundColor: theme.card,
    },
    optionSelected: { borderColor: theme.foreground },
    pressed: { opacity: 0.7 },
    radio: { ...mark, borderRadius: 10 },
    box: { ...mark, borderRadius: theme.radius.sm },
    markOn: { backgroundColor: theme.primary, borderColor: theme.primary },
    optionText: { flex: 1, gap: 2 },
    optionLabel: { ...theme.type.body, color: theme.foreground },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    label: { ...theme.type.label, color: theme.foregroundSubtle },
    otherField: { gap: theme.spacing[1.5] },
    noteField: { gap: theme.spacing[1.5] },
    input: {
      ...theme.type.body,
      color: theme.foreground,
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      paddingHorizontal: theme.spacing[3],
      paddingVertical: theme.spacing[2],
      minHeight: 44,
    },
    multiline: { minHeight: 88, textAlignVertical: 'top' },
    actions: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    button: { height: 44 },
    error: { ...theme.type.small, color: theme.status.danger },
  });
}
