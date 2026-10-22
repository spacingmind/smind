// DaemonPasswordPrompt.tsx asks for the daemon password (ADR-0031 P13c) when
// a relay daemon has one: "Daemon password required", "Incorrect password"
// after a refused attempt, and "Too many attempts" with a countdown while the
// daemon rate-limits this device. The typed text lives only in this
// component's local input until Submit hands it to the connection (memory
// only, see relay/daemonPasswords.ts); it is cleared on submit and never
// logged. The field is masked and sets autoComplete="off" so the OS has no
// reason to autofill it or offer to save it (best effort: how iOS/Android
// password managers treat it is not verified); the app itself never writes it
// to a keychain or any storage.
//
// Presentation: same Card anatomy as ConnectionStatusCard (StatusDot + title,
// detail) on the frozen Card primitive with a NEUTRAL border; the field sits
// in a token input box. Logic is unchanged.

import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { describePasswordState, PasswordPromptView } from '../relay/connectionStatus';
import { passwordProblem } from '../relay/daemonPasswords';
import { ConnectionState } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Card } from '../ui/Card';
import { StatusDot } from '../ui/StatusDot';
import { TextButton } from '../ui/TextButton';
import { UiHost } from '../ui/UiHost';
import { StatusAction } from './ConnectionStatusCard';
import { statusDotTone } from './statusCardView';

interface Props {
  state: ConnectionState;
  /** Called with the typed password (may throw for one the daemon could never accept, which is shown inline). */
  onSubmit: (password: string) => void;
  actions?: StatusAction[];
}

export function DaemonPasswordPrompt({ state, onSubmit, actions }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [now, setNow] = useState(() => Date.now());
  const [typed, setTyped] = useState('');
  const [problem, setProblem] = useState<string | null>(null);
  const field = useNativeState('');
  const locked = state.kind === 'locked';

  // The countdown only ticks while locked.
  useEffect(() => {
    if (!locked) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [locked]);

  const view: PasswordPromptView | null = describePasswordState(state, now);
  if (!view) return null;

  function submit() {
    const bad = passwordProblem(typed);
    if (bad) {
      setProblem(bad);
      return;
    }
    try {
      onSubmit(typed);
      setProblem(null);
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e));
      return;
    }
    // Do not keep the password in the field or in React state.
    field.value = '';
    setTyped('');
  }

  return (
    <Card style={styles.card}>
      <View style={styles.cardInner} accessibilityRole="alert">
        <View style={styles.titleRow}>
          <StatusDot tone={statusDotTone(view.tone)} />
          <Text style={styles.title}>{view.title}</Text>
        </View>
        <Text style={styles.detail}>{view.detail}</Text>
        {!view.locked && (
          <>
            <UiHost style={styles.inputHost}>
              <ExpoTextInput
                style={styles.inputBox}
                textStyle={styles.inputText}
                secureTextEntry
                autoCapitalize="none"
                autoCorrect={false}
                autoComplete="off"
                placeholder="Daemon password"
                placeholderTextColor={theme.foregroundSubtlest}
                value={field}
                onChangeText={(text) => {
                  field.value = text;
                  setTyped(text);
                  setProblem(null);
                }}
                onSubmitEditing={submit}
              />
            </UiHost>
            {problem ? <Text style={styles.problem}>{problem}</Text> : null}
          </>
        )}
        <View style={styles.actions}>
          {!view.locked && (
            <UiHost>
              <Button variant="filled" label="Connect" onPress={submit} disabled={typed.length === 0} style={{ height: 44 }} />
            </UiHost>
          )}
          {actions?.map((a) => (
            <TextButton key={a.label} label={a.label} onPress={a.onPress} />
          ))}
        </View>
      </View>
    </Card>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { marginTop: 0 },
    cardInner: { gap: theme.spacing[2] },
    titleRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    title: {
      fontSize: theme.type.label.fontSize,
      lineHeight: theme.type.label.lineHeight,
      fontWeight: theme.type.label.fontWeight,
      color: theme.foreground,
    },
    detail: {
      fontSize: theme.type.body.fontSize,
      lineHeight: theme.type.body.lineHeight,
      color: theme.foregroundSubtle,
    },
    inputHost: { alignSelf: 'stretch' },
    inputBox: {
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      minHeight: 44,
      paddingHorizontal: theme.spacing[3],
    },
    inputText: {
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize,
      color: theme.foreground,
    },
    problem: {
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.status.danger,
    },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: theme.spacing[2], marginTop: theme.spacing[2] },
  });
}
