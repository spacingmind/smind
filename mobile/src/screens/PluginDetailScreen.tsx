// PluginDetailScreen (ADR-0042 PA.5): one plugin's consent summary, READ-ONLY. It shows what the daemon returned for
// plugin.get and which prompt commands the plugin contributes; the escalating profiles are flagged. There is no
// control that changes a plugin (the Panels rows only navigate to a panel view); the only line about managing says
// where to do it. Plugin text is drawn as plain <Text> and never interpreted.

import { useCallback, useMemo } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { fetchPluginDetail, fetchPluginPanels } from '../plugins/api';
import { pluginDetailView, PluginDetailView } from '../plugins/listView';
import { panelsOfPlugin } from '../plugins/panelView';
import { usePluginResource } from '../plugins/usePluginResource';
import { pluginPanelHref } from '../navigation/routes';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Badge } from '../ui/Badge';
import { EmptyState } from '../ui/EmptyState';
import { LINK_WARNING_TEXT } from '../plugins/listView';
import { SectionHeader } from './tasks/SectionHeader';

export function PluginDetailScreen({ conn, id }: { conn: RelayConnection; id: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const router = useRouter();
  const load = useCallback((c: RelayConnection) => fetchPluginDetail(c, id), [id]);
  const { state } = usePluginResource(conn, load);
  // Only an ACTIVE plugin has panels listed (the daemon holds the rest back); loading or an error shows nothing.
  const panelsRes = usePluginResource(conn, fetchPluginPanels);
  const panelList = panelsRes.state.kind === 'ready' ? panelsOfPlugin(panelsRes.state.value, id) : [];

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load this plugin" body={state.message} />
      </View>
    );
  }
  const v = pluginDetailView(state.value.info, state.value.commands);
  const link = state.value.info.sourceKind === 'link';

  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <Header v={v} styles={styles} />
      {link && <Text style={styles.warning}>{LINK_WARNING_TEXT}</Text>}
      {v.pendingNotice !== null && <Text style={styles.warning}>{v.pendingNotice}</Text>}
      {v.header.lastError !== null && <Text style={styles.error}>{v.header.lastError}</Text>}
      <Text style={styles.line}>{v.dataOnly ? 'Data only: nothing it declares runs code.' : 'Runs code: it declares a process or hooks.'}</Text>
      {v.process.length > 0 && <Text style={styles.mono}>{v.process.join(' ')}</Text>}
      {panelList.length > 0 && (
        <Section title="Panels" styles={styles}>
          {panelList.map((p) => (
            <Pressable
              key={p.id}
              style={styles.item}
              accessibilityRole="button"
              accessibilityLabel={p.title}
              onPress={() => router.push(pluginPanelHref(id, p.id))}
            >
              <Text style={styles.itemMain}>{p.title}</Text>
              <Text style={styles.itemSub}>{`${p.where} panel`}</Text>
            </Pressable>
          ))}
        </Section>
      )}
      {v.capabilities.length > 0 && (
        <Section title="Capabilities" styles={styles}>
          <View style={styles.chips}>
            {v.capabilities.map((c) => (
              <Badge key={c}>{c}</Badge>
            ))}
          </View>
        </Section>
      )}
      {v.mcpServers.length > 0 && (
        <Section title="MCP servers" styles={styles}>
          {v.mcpServers.map((m) => (
            <Item key={m.name} main={m.name} sub={`${m.transport} · ${m.owner}`} styles={styles} />
          ))}
        </Section>
      )}
      {v.skills.length > 0 && (
        <Section title="Skills" styles={styles}>
          {v.skills.map((s) => (
            <Item key={s.name} main={s.name} sub={s.owner} styles={styles} />
          ))}
        </Section>
      )}
      {v.profiles.length > 0 && (
        <Section title="Profiles" styles={styles}>
          {v.profiles.map((p) => (
            <View key={p.name} style={styles.item}>
              <Text style={styles.itemMain}>{p.name}</Text>
              <Text style={styles.itemSub}>{`${p.provider} · ${p.owner}`}</Text>
              {p.escalations.map((e) => (
                <Text key={e} style={styles.flag}>{`Needs your attention: ${e}`}</Text>
              ))}
            </View>
          ))}
        </Section>
      )}
      {v.commands.length > 0 && (
        <Section title="Prompt commands" styles={styles}>
          {v.commands.map((c) => (
            <Item key={c.label} main={c.label} sub={c.owner} styles={styles} />
          ))}
        </Section>
      )}
      {v.conflicts.length > 0 && (
        <Section title="Conflicts" styles={styles}>
          {v.conflicts.map((c) => (
            <Item key={`${c.kind}:${c.name}`} main={`${c.kind} ${c.name}`} sub={c.reason} styles={styles} />
          ))}
        </Section>
      )}
      <Text style={styles.hint}>{v.manageHint}</Text>
    </ScrollView>
  );
}

type Styles = ReturnType<typeof makeStyles>;

function Header({ v, styles }: { v: PluginDetailView; styles: Styles }) {
  return (
    <View style={styles.header}>
      <Text style={styles.title}>{v.header.name}</Text>
      <Text style={styles.muted}>{`${v.header.version} · ${v.source}`}</Text>
      {v.description !== null && <Text style={styles.line}>{v.description}</Text>}
      <View style={styles.chips}>
        <Badge>{v.header.source}</Badge>
        <Badge tone={v.header.tone}>{v.header.status}</Badge>
      </View>
    </View>
  );
}

function Section({ title, styles, children }: { title: string; styles: Styles; children: React.ReactNode }) {
  return (
    <View>
      <SectionHeader title={title} />
      <View style={styles.card}>{children}</View>
    </View>
  );
}

function Item({ main, sub, styles }: { main: string; sub: string; styles: Styles }) {
  return (
    <View style={styles.item}>
      <Text style={styles.itemMain}>{main}</Text>
      <Text style={styles.itemSub}>{sub}</Text>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[2] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    header: { gap: theme.spacing[1.5] },
    title: { ...theme.type.heading1, color: theme.foreground },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    line: { ...theme.type.body, color: theme.foreground },
    mono: { ...theme.type.mono, color: theme.foregroundSubtle },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: theme.spacing[2] },
    warning: { ...theme.type.small, color: theme.status.warning },
    error: { ...theme.type.small, color: theme.status.danger },
    flag: { ...theme.type.small, color: theme.status.warning },
    card: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
      gap: theme.spacing[3],
    },
    item: { gap: 2 },
    itemMain: { ...theme.type.label, color: theme.foreground },
    itemSub: { ...theme.type.small, color: theme.foregroundSubtle },
    hint: { ...theme.type.small, color: theme.foregroundSubtle, marginTop: theme.spacing[3] },
  });
}
