// DaemonPasswordPrompt.tsx asks for the daemon password (ADR-0031 P13c) when
// a relay daemon has one: "Daemon password required", "Incorrect password"
// after a refused attempt, and "Too many attempts" with a countdown while the
// daemon rate-limits this device. The typed text lives only in this
// component's local input until Submit hands it to the connection (memory
// only, see relay/daemonPasswords.ts); it is cleared on submit and never
// logged. The field asks the OS not to autofill or offer to save it: the
// password must not reach a keychain or password manager from here.

import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { describePasswordState, PasswordPromptView } from '../relay/connectionStatus';
import { passwordProblem } from '../relay/daemonPasswords';
import { ConnectionState } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { UiHost } from '../ui/UiHost';
import { StatusAction } from './ConnectionStatusCard';

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
  const accent = theme.status[view.tone === 'danger' ? 'danger' : 'warning'];

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
    <View style={[styles.card, { borderColor: accent }]} accessibilityRole="alert">
      <View style={styles.titleRow}>
        <View style={[styles.dot, { backgroundColor: accent }]} />
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
              placeholderTextColor={theme.foregroundMuted}
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
            <Button label="Connect" onPress={submit} disabled={typed.length === 0} />
          </UiHost>
        )}
        {actions?.map((a) => (
          <UiHost key={a.label}>
            <Button label={a.label} onPress={a.onPress} />
          </UiHost>
        ))}
      </View>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      borderWidth: 1,
      borderRadius: theme.radius.md,
      backgroundColor: theme.surface[1],
      padding: theme.spacing[4],
      marginTop: theme.spacing[4],
      gap: theme.spacing[2],
    },
    titleRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    dot: { width: 6, height: 6, borderRadius: theme.radius.full },
    title: {
      fontSize: theme.type.panelTitle.fontSize,
      lineHeight: theme.type.panelTitle.lineHeight,
      fontWeight: theme.type.panelTitle.fontWeight,
      color: theme.foreground,
    },
    detail: {
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      color: theme.foregroundMuted,
    },
    inputHost: { alignSelf: 'stretch' },
    inputBox: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
    },
    inputText: {
      fontSize: theme.type.interface.fontSize,
      color: theme.foreground,
    },
    problem: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      color: theme.status.danger,
    },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: theme.spacing[2], marginTop: theme.spacing[2] },
  });
}
