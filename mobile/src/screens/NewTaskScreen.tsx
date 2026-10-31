// NewTaskScreen is the create-a-task form (docs/plans/active/
// mobile-create-task.md): a keyboard-safe ScrollView of Card sections
// (PairingScreen's rhythm) whose submit runs newTask.ts's pure pipeline.
// All submit state lives here; pure shaping lives in newTaskForm.ts and
// providerCatalog.ts. Partial failure (task created, first run failed)
// swaps the actions to Retry first prompt + Open task -- task.create is
// never called again once a task exists.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Button, Picker, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { ProviderInfo, Space, Task, Workspace, listProviders, listWorkspaces } from '../api';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { hapticFor } from '../haptics';
import {
  NewTaskInput,
  SubmitResult,
  clampPrompt,
  clampTitle,
  retryFirstRun,
  singleFlight,
  submitNewTask,
} from '../newTask';
import { NewTaskSelection, catalogOrFallback, defaultSelection, modeOptions, modelOptions, providerLabel, providerNote, selectProvider } from '../providerCatalog';
import { recallSelection, rememberSelection } from '../newTaskPrefs';
import { issueMessage, spacesFor } from '../newTaskForm';
import { taskHref } from '../navigation/routes';
import { Card } from '../ui/Card';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { UiHost } from '../ui/UiHost';
import { FieldRow } from './newtask/FieldRow';
import { StartModeField } from './newtask/StartModeField';

type Load =
  | { kind: 'loading' }
  | { kind: 'error' }
  | { kind: 'ready'; workspaces: Workspace[]; spaces: Space[]; providers: ProviderInfo[] };

export function NewTaskScreen({ conn }: { conn: RelayConnection }) {
  const theme = useAppTheme();
  const router = useRouter();
  const styles = useMemo(() => makeStyles(theme), [theme]);

  const [load, setLoad] = useState<Load>({ kind: 'loading' });
  const [workspaceId, setWorkspaceId] = useState<number | null>(null);
  const [spaceId, setSpaceId] = useState<number | null>(null);
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  // @expo/ui's TextInput binds text via ObservableState (PairingScreen's note); React state stays the source of truth.
  const titleState = useNativeState('');
  const promptState = useNativeState('');
  const [selection, setSelection] = useState<NewTaskSelection | null>(null);
  const [start, setStart] = useState<NewTaskInput['start']>({ kind: 'head' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The task that already exists after a run-failed partial failure: task.create must never run again.
  const [created, setCreated] = useState<Task | null>(null);

  const [reload, setReload] = useState(0);
  useEffect(() => {
    let alive = true;
    setLoad({ kind: 'loading' });
    (async () => {
      try {
        // A lost provider.list must not strand the form: the offline built-ins take over (workspaces still must load).
        const [workspaces, served] = await Promise.all([listWorkspaces(conn), listProviders(conn).catch(() => null)]);
        const providers = catalogOrFallback(served);
        const first = workspaces[0];
        const spaces = first ? await conn.call('space.list', { workspaceId: first.ID }) : [];
        if (!alive) return;
        setLoad({ kind: 'ready', workspaces, spaces: spaces as Space[], providers });
        setWorkspaceId(first ? first.ID : null);
        setSelection(defaultSelection(providers, recallSelection()) ?? defaultSelection(providers, {}));
      } catch {
        if (alive) setLoad({ kind: 'error' });
      }
    })();
    return () => {
      alive = false;
    };
  }, [conn, reload]);

  const ready = load.kind === 'ready' ? load : null;
  const spaces = useMemo(() => (ready && workspaceId !== null ? spacesFor(ready.spaces, workspaceId) : []), [ready, workspaceId]);
  const provider = useMemo(() => ready?.providers.find((p) => p.id === selection?.provider), [ready, selection]);
  const note = providerNote(provider);

  const input = useMemo<NewTaskInput>(
    () => ({
      workspaceId: workspaceId ?? 0,
      spaceId,
      title,
      prompt,
      provider: selection?.provider ?? '',
      model: selection?.model ?? '',
      mode: selection?.mode ?? '',
      start,
    }),
    [workspaceId, spaceId, title, prompt, selection, start],
  );

  const disabled = busy || title.trim().length === 0 || created !== null;

  const openTask = useCallback(
    (task: { ID: number; Title: string }) => {
      router.dismiss();
      router.push(taskHref(task));
    },
    [router],
  );

  const after = useCallback(
    (r: SubmitResult) => {
      if (r.kind === 'ok') {
        openTask(r.task);
        return;
      }
      setBusy(false);
      if (r.kind === 'invalid') {
        setError(issueMessage(r.issues[0]));
        return;
      }
      hapticFor('error');
      if (r.kind === 'create-failed') {
        setError(r.message);
        return;
      }
      setCreated(r.task);
      setError(`Task #${r.task.ID} was created, but the first prompt failed: ${r.message}`);
    },
    [openTask],
  );

  const handleCreate = useCallback(async () => {
    setBusy(true);
    setError('');
    if (selection) rememberSelection(selection);
    hapticFor('send');
    after(await submitNewTask(conn, input, ready?.providers ?? []));
  }, [after, conn, input, ready, selection]);

  const handleRetry = useCallback(async () => {
    if (!created) return;
    setBusy(true);
    setError('');
    if (selection) rememberSelection(selection);
    hapticFor('send');
    after(await retryFirstRun(conn, created, input, ready?.providers ?? []));
  }, [after, conn, created, input, ready, selection]);

  const submit = useMemo(() => singleFlight(handleCreate), [handleCreate]);
  const retry = useMemo(() => singleFlight(handleRetry), [handleRetry]);

  if (load.kind === 'loading') {
    return (
      <ScrollView style={styles.container} contentContainerStyle={styles.content} contentInsetAdjustmentBehavior="automatic">
        <Card>
          <Text style={styles.loading}>Loading…</Text>
        </Card>
      </ScrollView>
    );
  }
  if (load.kind === 'error') {
    return (
      <ScrollView style={styles.container} contentContainerStyle={styles.content} contentInsetAdjustmentBehavior="automatic">
        <EmptyState title="Couldn't load the form" body="The daemon did not answer the workspace or provider list.">
          <UiHost>
            <Button variant="filled" label="Retry" onPress={() => setReload((n) => n + 1)} style={{ height: 44 }} />
          </UiHost>
        </EmptyState>
      </ScrollView>
    );
  }

  const { workspaces, providers } = load;
  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={styles.content}
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps="handled"
    >
      <Card style={styles.formCard}>
        {workspaces.length > 1 ? (
          <FieldRow label="Workspace">
            <UiHost style={styles.controlHost}>
              <Picker
                selectedValue={workspaceId ?? ''}
                onValueChange={(v) => {
                  setWorkspaceId(Number(v));
                  setSpaceId(null);
                  // Changing workspace reloads its spaces; until then the Space picker hides.
                  conn
                    .call('space.list', { workspaceId: Number(v) })
                    .then((s) => setLoad((l) => (l.kind === 'ready' ? { ...l, spaces: s as Space[] } : l)))
                    .catch(() => {});
                }}
              >
                {workspaces.map((w) => (
                  <Picker.Item key={w.ID} label={w.Title} value={w.ID} />
                ))}
              </Picker>
            </UiHost>
          </FieldRow>
        ) : null}
        {spaces.length > 0 ? (
          <FieldRow label="Space">
            <UiHost style={styles.controlHost}>
              <Picker selectedValue={spaceId ?? ''} onValueChange={(v) => setSpaceId(v === '' ? null : Number(v))}>
                <Picker.Item label="None" value="" />
                {spaces.map((s) => (
                  <Picker.Item key={s.ID} label={s.Title} value={s.ID} />
                ))}
              </Picker>
            </UiHost>
          </FieldRow>
        ) : null}
        <FieldRow label="Title">
          <UiHost style={styles.controlHost}>
            <ExpoTextInput
              style={styles.inputBox}
              textStyle={styles.inputText}
              placeholder="Task title"
              placeholderTextColor={theme.foregroundSubtlest}
              value={titleState}
              onChangeText={(t) => {
                const clamped = clampTitle(t);
                titleState.value = clamped;
                setTitle(clamped);
              }}
            />
          </UiHost>
        </FieldRow>
        <FieldRow label="First prompt">
          <UiHost style={styles.controlHost}>
            <ExpoTextInput
              style={{ ...styles.inputBox, ...styles.promptBox }}
              textStyle={styles.inputText}
              multiline
              numberOfLines={3}
              placeholder="What should the agent do? (optional)"
              placeholderTextColor={theme.foregroundSubtlest}
              value={promptState}
              onChangeText={(t) => {
                const clamped = clampPrompt(t);
                promptState.value = clamped;
                setPrompt(clamped);
              }}
            />
          </UiHost>
        </FieldRow>
        <StartModeField start={start} onChange={setStart} />
      </Card>

      {providers.length > 0 && selection && provider ? (
        <Card style={styles.formCard}>
          <FieldRow label="Provider">
            <UiHost style={styles.controlHost}>
              <Picker
                selectedValue={selection.provider}
                onValueChange={(v) => setSelection(selectProvider(providers, String(v), recallSelection()))}
              >
                {providers.map((p) => (
                  <Picker.Item key={p.id} label={providerLabel(p)} value={p.id} />
                ))}
              </Picker>
            </UiHost>
          </FieldRow>
          {note ? (
            <Text style={styles.note} testID="provider-note">
              {note}
            </Text>
          ) : null}
          <FieldRow label="Model">
            <UiHost style={styles.controlHost}>
              <Picker selectedValue={selection.model} onValueChange={(v) => setSelection({ ...selection, model: String(v) })}>
                {modelOptions(provider).map((o) => (
                  <Picker.Item key={o.value} label={o.label} value={o.value} />
                ))}
              </Picker>
            </UiHost>
          </FieldRow>
          <FieldRow label="Permission mode">
            <UiHost style={styles.controlHost}>
              <Picker selectedValue={selection.mode} onValueChange={(v) => setSelection({ ...selection, mode: String(v) })}>
                {modeOptions(provider).map((o) => (
                  <Picker.Item key={o.value} label={o.label} value={o.value} />
                ))}
              </Picker>
            </UiHost>
          </FieldRow>
        </Card>
      ) : (
        <Card>
          <Text style={styles.note}>This daemon has no providers; the task can still be created.</Text>
        </Card>
      )}

      {created ? (
        <View style={styles.partialActions}>
          <UiHost style={styles.submitHost}>
            <Button variant="filled" label={busy ? 'Retrying…' : 'Retry first prompt'} onPress={retry} disabled={busy} style={{ height: 44 }} />
          </UiHost>
          <TextButton label="Open task" onPress={() => openTask(created)} />
        </View>
      ) : (
        <UiHost style={styles.submitHost}>
          <Button variant="filled" label={busy ? 'Creating…' : 'Create task'} onPress={submit} disabled={disabled} style={{ height: 44 }} />
        </UiHost>
      )}
      {error.length > 0 ? (
        <Text style={styles.error} accessibilityRole="alert" selectable>
          {error}
        </Text>
      ) : null}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], gap: theme.spacing[4] },
    formCard: { gap: theme.spacing[3] },
    controlHost: { alignSelf: 'stretch' },
    inputBox: {
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    promptBox: { minHeight: 88 },
    inputText: {
      // Web: 16px keeps iOS Safari from zooming on focus (expo-native-ui input floor).
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize,
      color: theme.foreground,
    },
    note: { ...theme.type.small, color: theme.foregroundSubtle },
    loading: { ...theme.type.body, color: theme.foregroundSubtle },
    submitHost: { alignSelf: 'stretch' },
    partialActions: { gap: theme.spacing[3], alignSelf: 'stretch' },
    error: {
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.status.danger,
    },
  });
}
