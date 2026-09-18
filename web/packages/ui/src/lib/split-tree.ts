import type { TabEntry } from "@/components/tab-registry";

/**
 * Ported from paseo's `workspace-layout-actions.ts` (packages/app/src/stores,
 * commit 3cc4ae2) per docs/plans/active/pane-split-tree.md Item 1. Adapted for
 * smind: `SplitPane` carries smind's richer `TabEntry` objects directly
 * (`tabs`) instead of paseo's separate `tabIds`/`WorkspaceTab` split, the
 * layout type is renamed `TaskLayout` and drops `parentTabIdByTabId`
 * (paseo-only), and there is no pane-hiding / Explorer-sidebar concept.
 */

export const MIN_SPLIT_SIZE = 0.1;
export const MAX_TREE_DEPTH = 5;

export const DEFAULT_PANE_ID = "primary";

export interface SplitPane {
  id: string;
  tabs: TabEntry[];
  activeKey: string | null;
}

export interface SplitGroup {
  id: string;
  direction: "horizontal" | "vertical";
  children: SplitNode[];
  sizes: number[];
}

export type SplitNode = { kind: "pane"; pane: SplitPane } | { kind: "group"; group: SplitGroup };

export interface TaskLayout {
  root: SplitNode;
  focusedPaneId: string | null;
}

function assert(cond: boolean, msg: string): asserts cond {
  if (!cond) {
    throw new Error(msg);
  }
}

interface NormalizeSizesInput {
  sizes: number[];
  count: number;
}

function normalizeSizes(input: NormalizeSizesInput): number[] {
  if (input.count <= 0) {
    return [];
  }

  const raw = input.sizes.slice(0, input.count);
  while (raw.length < input.count) {
    raw.push(1);
  }

  const sanitized = raw.map((value) => (Number.isFinite(value) && value > 0 ? value : 1));
  const total = sanitized.reduce((sum, value) => sum + value, 0);
  if (total <= 0) {
    return Array.from({ length: input.count }, () => 1 / input.count);
  }
  return sanitized.map((value) => value / total);
}

export function clampNormalizedSizes(sizes: number[]): number[] {
  if (sizes.length === 0) {
    return [];
  }

  const normalized = normalizeSizes({ sizes, count: sizes.length });
  if (sizes.length === 1) {
    return [1];
  }
  if (sizes.length * MIN_SPLIT_SIZE > 1) {
    return Array.from({ length: sizes.length }, () => 1 / sizes.length);
  }

  const nextSizes = Array.from({ length: sizes.length }, () => 0);
  const unlocked = new Set(normalized.map((_, index) => index));
  let remainingTotal = 1;

  while (unlocked.size > 0) {
    let unlockedWeight = 0;
    for (const index of unlocked) {
      unlockedWeight += normalized[index] ?? 0;
    }

    if (unlockedWeight <= 0) {
      const evenShare = remainingTotal / unlocked.size;
      for (const index of unlocked) {
        nextSizes[index] = evenShare;
      }
      break;
    }

    const nextLocked: number[] = [];
    for (const index of unlocked) {
      const proposedSize = ((normalized[index] ?? 0) / unlockedWeight) * remainingTotal;
      if (proposedSize < MIN_SPLIT_SIZE) {
        nextLocked.push(index);
      }
    }

    if (nextLocked.length === 0) {
      for (const index of unlocked) {
        nextSizes[index] = ((normalized[index] ?? 0) / unlockedWeight) * remainingTotal;
      }
      break;
    }

    for (const index of nextLocked) {
      nextSizes[index] = MIN_SPLIT_SIZE;
      unlocked.delete(index);
      remainingTotal -= MIN_SPLIT_SIZE;
    }
  }

  return normalizeSizes({ sizes: nextSizes, count: nextSizes.length });
}

function createPaneNode(input: { id: string; tabs?: TabEntry[]; activeKey?: string | null }): SplitNode {
  const tabs = input.tabs ?? [];
  const keys = tabs.map((tab) => tab.key);
  const activeKey = keys.includes(input.activeKey ?? "")
    ? (input.activeKey ?? null)
    : (keys[keys.length - 1] ?? null);
  return { kind: "pane", pane: { id: input.id, tabs, activeKey } };
}

function createGroupNode(input: {
  id: string;
  direction: "horizontal" | "vertical";
  children: SplitNode[];
  sizes?: number[];
}): SplitNode {
  return {
    kind: "group",
    group: {
      id: input.id,
      direction: input.direction,
      children: input.children,
      sizes: normalizeSizes({
        sizes: input.sizes ?? input.children.map(() => 1 / Math.max(input.children.length, 1)),
        count: input.children.length,
      }),
    },
  };
}

function normalizePaneAfterTabChange(pane: SplitPane): SplitPane {
  const keys = pane.tabs.map((tab) => tab.key);
  const activeKey = keys.includes(pane.activeKey ?? "") ? pane.activeKey : (keys[keys.length - 1] ?? null);
  return { id: pane.id, tabs: pane.tabs, activeKey };
}

function findPanePathById(node: SplitNode, paneId: string, path: number[] = []): number[] | null {
  if (node.kind === "pane") {
    return node.pane.id === paneId ? path : null;
  }
  for (let index = 0; index < node.group.children.length; index += 1) {
    const childPath = findPanePathById(node.group.children[index]!, paneId, [...path, index]);
    if (childPath) {
      return childPath;
    }
  }
  return null;
}

function findPanePathContainingTab(node: SplitNode, tabKey: string, path: number[] = []): number[] | null {
  if (node.kind === "pane") {
    return node.pane.tabs.some((tab) => tab.key === tabKey) ? path : null;
  }
  for (let index = 0; index < node.group.children.length; index += 1) {
    const childPath = findPanePathContainingTab(node.group.children[index]!, tabKey, [...path, index]);
    if (childPath) {
      return childPath;
    }
  }
  return null;
}

function findGroupPathById(node: SplitNode, groupId: string, path: number[] = []): number[] | null {
  if (node.kind === "pane") {
    return null;
  }
  if (node.group.id === groupId) {
    return path;
  }
  for (let index = 0; index < node.group.children.length; index += 1) {
    const childPath = findGroupPathById(node.group.children[index]!, groupId, [...path, index]);
    if (childPath) {
      return childPath;
    }
  }
  return null;
}

/**
 * The group holding the node at `targetPath`, or null when that node is the whole tree.
 *
 * Ask `targetPath`, never the parent path: `targetPath.slice(0, -1)` is empty both for "this node
 * is the root" and for "this node is a direct child of the root group". Treating the second case as
 * parentless wraps root-level panes in a redundant group on every split.
 */
function findParentGroup(root: SplitNode, targetPath: number[]): SplitNode | null {
  if (targetPath.length === 0) {
    return null;
  }
  return getNodeAtPath(root, targetPath.slice(0, -1));
}

function getNodeAtPath(node: SplitNode, path: number[]): SplitNode {
  let current = node;
  for (const index of path) {
    assert(current.kind === "group", "Expected group while traversing split tree");
    current = current.group.children[index]!;
  }
  return current;
}

function replaceNodeAtPath(
  node: SplitNode,
  path: number[],
  updater: (node: SplitNode) => SplitNode,
): SplitNode {
  if (path.length === 0) {
    return updater(node);
  }

  assert(node.kind === "group", "Expected group while replacing split tree node");
  const [index, ...rest] = path;
  const nextChildren = node.group.children.map((child, childIndex) =>
    childIndex === index ? replaceNodeAtPath(child, rest, updater) : child,
  );

  return createGroupNode({
    id: node.group.id,
    direction: node.group.direction,
    children: nextChildren,
    sizes: node.group.sizes,
  });
}

interface InsertChildIntoGroupInput {
  index: number;
  node: SplitNode;
  sizes: number[];
}

function insertChildIntoGroup(groupNode: SplitNode, input: InsertChildIntoGroupInput): SplitNode {
  assert(groupNode.kind === "group", "Expected group for split insertion");
  const nextChildren = groupNode.group.children.slice();
  nextChildren.splice(input.index, 0, input.node);
  return createGroupNode({
    id: groupNode.group.id,
    direction: groupNode.group.direction,
    children: nextChildren,
    sizes: input.sizes,
  });
}

function listPaneIds(node: SplitNode): string[] {
  if (node.kind === "pane") {
    return [node.pane.id];
  }
  const next: string[] = [];
  for (const child of node.group.children) {
    next.push(...listPaneIds(child));
  }
  return next;
}

function findNearestSiblingPaneId(root: SplitNode, paneId: string): string | null {
  const path = findPanePathById(root, paneId);
  if (!path || path.length === 0) {
    return null;
  }

  for (let depth = path.length - 1; depth >= 0; depth -= 1) {
    const parentPath = path.slice(0, depth);
    const childIndex = path[depth]!;
    const parentNode = getNodeAtPath(root, parentPath);
    assert(parentNode.kind === "group", "Expected parent group for pane lookup");

    for (let index = childIndex - 1; index >= 0; index -= 1) {
      const paneIds = listPaneIds(parentNode.group.children[index]!);
      if (paneIds.length > 0) {
        return paneIds[paneIds.length - 1] ?? null;
      }
    }

    for (let index = childIndex + 1; index < parentNode.group.children.length; index += 1) {
      const paneIds = listPaneIds(parentNode.group.children[index]!);
      if (paneIds.length > 0) {
        return paneIds[0] ?? null;
      }
    }
  }

  return null;
}

function removePaneByPath(root: SplitNode, path: number[]): SplitNode {
  if (path.length === 0) {
    assert(root.kind === "pane", "Expected pane at root while removing pane");
    return { kind: "pane", pane: { ...root.pane, tabs: [], activeKey: null } };
  }

  const parentPath = path.slice(0, -1);
  const removeIndex = path[path.length - 1]!;
  const parentNode = getNodeAtPath(root, parentPath);
  assert(parentNode.kind === "group", "Expected parent group while removing pane");

  const nextParentChildren = parentNode.group.children.filter((_, index) => index !== removeIndex);
  assert(nextParentChildren.length > 0, "Split tree cannot remove the final pane");

  const nextParentNode =
    nextParentChildren.length === 1
      ? nextParentChildren[0]!
      : createGroupNode({
          id: parentNode.group.id,
          direction: parentNode.group.direction,
          children: nextParentChildren,
          sizes: parentNode.group.sizes.filter((_, index) => index !== removeIndex),
        });

  return replaceNodeAtPath(root, parentPath, () => nextParentNode);
}

interface DetachTabFromTreeResult {
  root: SplitNode;
  tab: TabEntry | null;
  sourcePaneId: string | null;
}

function detachTabFromTree(
  root: SplitNode,
  input: { tabKey: string; preserveEmptyPaneId?: string | null },
): DetachTabFromTreeResult {
  const panePath = findPanePathContainingTab(root, input.tabKey);
  if (!panePath) {
    return { root, tab: null, sourcePaneId: null };
  }

  const paneNode = getNodeAtPath(root, panePath);
  assert(paneNode.kind === "pane", "Expected pane while detaching tab");
  const tab = paneNode.pane.tabs.find((entry) => entry.key === input.tabKey) ?? null;
  if (!tab) {
    return { root, tab: null, sourcePaneId: paneNode.pane.id };
  }

  const nextPane = normalizePaneAfterTabChange({
    ...paneNode.pane,
    tabs: paneNode.pane.tabs.filter((entry) => entry.key !== input.tabKey),
  });

  const nextRoot = replaceNodeAtPath(root, panePath, () => ({ kind: "pane", pane: nextPane }));
  if (nextPane.tabs.length > 0 || nextPane.id === input.preserveEmptyPaneId) {
    return { root: nextRoot, tab, sourcePaneId: paneNode.pane.id };
  }

  return {
    root: removePaneByPath(nextRoot, panePath),
    tab,
    sourcePaneId: paneNode.pane.id,
  };
}

interface InsertTabIntoPaneInput {
  insertionPosition?: { afterTabKey: string };
  paneId: string;
  tab: TabEntry;
  focusTabKey?: string | null;
}

function insertTabIntoPane(root: SplitNode, input: InsertTabIntoPaneInput): SplitNode {
  const panePath = findPanePathById(root, input.paneId);
  assert(panePath !== null, `Pane not found: ${input.paneId}`);
  return replaceNodeAtPath(root, panePath, (node) => {
    assert(node.kind === "pane", "Expected pane while inserting tab");
    const existingIndex = node.pane.tabs.findIndex((tab) => tab.key === input.tab.key);
    let nextTabs: TabEntry[];
    if (existingIndex >= 0) {
      nextTabs = node.pane.tabs.map((tab, index) => (index === existingIndex ? input.tab : tab));
    } else {
      nextTabs = [...node.pane.tabs];
      const afterIndex = nextTabs.findIndex((tab) => tab.key === input.insertionPosition?.afterTabKey);
      nextTabs.splice(afterIndex >= 0 ? afterIndex + 1 : nextTabs.length, 0, input.tab);
    }
    return {
      kind: "pane",
      pane: normalizePaneAfterTabChange({
        ...node.pane,
        tabs: nextTabs,
        activeKey: input.focusTabKey ?? input.tab.key,
      }),
    };
  });
}

function focusTabInPane(root: SplitNode, paneId: string, tabKey: string): SplitNode {
  const panePath = findPanePathById(root, paneId);
  assert(panePath !== null, `Pane not found: ${paneId}`);
  return replaceNodeAtPath(root, panePath, (node) => {
    assert(node.kind === "pane", "Expected pane while focusing tab");
    return {
      kind: "pane",
      pane: normalizePaneAfterTabChange({ ...node.pane, activeKey: tabKey }),
    };
  });
}

function updateGroupSizesInTree(root: SplitNode, input: { groupId: string; sizes: number[] }): SplitNode {
  const groupPath = findGroupPathById(root, input.groupId);
  if (!groupPath) {
    return root;
  }
  return replaceNodeAtPath(root, groupPath, (node) => {
    assert(node.kind === "group", "Expected group while resizing split");
    if (input.sizes.length !== node.group.children.length) {
      return node;
    }
    return createGroupNode({
      id: node.group.id,
      direction: node.group.direction,
      children: node.group.children,
      sizes: clampNormalizedSizes(input.sizes),
    });
  });
}

export function updatePaneInTree(
  root: SplitNode,
  input: { paneId: string; updater: (pane: SplitPane) => SplitPane },
): SplitNode {
  const panePath = findPanePathById(root, input.paneId);
  if (!panePath) {
    return root;
  }
  return replaceNodeAtPath(root, panePath, (node) => {
    assert(node.kind === "pane", "Expected pane while updating pane");
    return { kind: "pane", pane: normalizePaneAfterTabChange(input.updater(node.pane)) };
  });
}

interface InsertSplitInternalResult {
  root: SplitNode;
  newPaneId: string;
}

function insertSplitInternal(input: {
  root: SplitNode;
  targetPaneId: string;
  tabKey: string;
  position: "left" | "right" | "top" | "bottom";
  createNodeId: (prefix: "pane" | "group") => string;
}): InsertSplitInternalResult {
  const direction = input.position === "left" || input.position === "right" ? "horizontal" : "vertical";
  const insertAfter = input.position === "right" || input.position === "bottom";

  const targetPathBeforeDetach = findPanePathById(input.root, input.targetPaneId);
  assert(targetPathBeforeDetach !== null, `Target pane not found: ${input.targetPaneId}`);

  const detached = detachTabFromTree(input.root, {
    tabKey: input.tabKey,
    preserveEmptyPaneId: input.targetPaneId,
  });
  assert(detached.tab !== null, `Tab not found: ${input.tabKey}`);

  const targetPath = findPanePathById(detached.root, input.targetPaneId);
  assert(targetPath !== null, `Target pane not found after detach: ${input.targetPaneId}`);
  const targetNode = getNodeAtPath(detached.root, targetPath);
  assert(targetNode.kind === "pane", "Expected target pane after detach");

  const newPaneId = input.createNodeId("pane");
  const newPaneNode = createPaneNode({ id: newPaneId, tabs: [detached.tab], activeKey: detached.tab.key });

  const parentPath = targetPath.slice(0, -1);
  const targetIndex = targetPath[targetPath.length - 1] ?? 0;
  const parentNode = findParentGroup(detached.root, targetPath);

  if (parentNode?.kind === "group" && parentNode.group.direction === direction) {
    const targetSize = parentNode.group.sizes[targetIndex] ?? 0;
    const nextSizes = parentNode.group.sizes.slice();
    const insertIndex = insertAfter ? targetIndex + 1 : targetIndex;
    nextSizes.splice(insertIndex, 0, targetSize / 2);
    nextSizes[targetIndex + (insertAfter ? 0 : 1)] = targetSize / 2;

    return {
      root: replaceNodeAtPath(detached.root, parentPath, () =>
        insertChildIntoGroup(parentNode, { index: insertIndex, node: newPaneNode, sizes: nextSizes }),
      ),
      newPaneId,
    };
  }

  const newGroup = createGroupNode({
    id: input.createNodeId("group"),
    direction,
    children: insertAfter ? [targetNode, newPaneNode] : [newPaneNode, targetNode],
    sizes: [0.5, 0.5],
  });

  return {
    root: replaceNodeAtPath(detached.root, targetPath, () => newGroup),
    newPaneId,
  };
}

function trimNonEmpty(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function isTabEntryLike(value: unknown): value is TabEntry {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const v = value as Record<string, unknown>;
  return (
    typeof v.kind === "string" &&
    typeof v.key === "string" &&
    typeof v.title === "string" &&
    typeof v.taskId === "number"
  );
}

function normalizeTabs(value: unknown): TabEntry[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const next: TabEntry[] = [];
  const seen = new Set<string>();
  for (const entry of value) {
    if (!isTabEntryLike(entry) || seen.has(entry.key)) {
      continue;
    }
    seen.add(entry.key);
    next.push(entry);
  }
  return next;
}

function normalizePaneNode(rawPane: unknown): SplitNode | null {
  if (typeof rawPane !== "object" || rawPane === null) {
    return null;
  }
  const v = rawPane as Record<string, unknown>;
  const paneId = trimNonEmpty(v.id);
  if (!paneId) {
    return null;
  }
  return createPaneNode({
    id: paneId,
    tabs: normalizeTabs(v.tabs),
    activeKey: trimNonEmpty(v.activeKey) ?? null,
  });
}

function normalizeGroupNode(rawGroup: unknown): SplitNode | null {
  if (typeof rawGroup !== "object" || rawGroup === null) {
    return null;
  }
  const v = rawGroup as Record<string, unknown>;
  const groupId = trimNonEmpty(v.id);
  const direction = v.direction;
  if (!groupId || (direction !== "horizontal" && direction !== "vertical")) {
    return null;
  }

  const children = Array.isArray(v.children)
    ? v.children.map((child) => normalizeNode(child)).filter((child): child is SplitNode => child !== null)
    : [];
  if (children.length === 0) {
    return null;
  }
  if (children.length === 1) {
    return children[0] ?? null;
  }

  return createGroupNode({
    id: groupId,
    direction,
    children,
    sizes: Array.isArray(v.sizes) ? (v.sizes as number[]) : [],
  });
}

function normalizeNode(node: unknown): SplitNode | null {
  if (!node || typeof node !== "object") {
    return null;
  }
  const kind = (node as { kind?: unknown }).kind;
  if (kind === "pane") {
    return normalizePaneNode((node as { pane?: unknown }).pane);
  }
  if (kind === "group") {
    return normalizeGroupNode((node as { group?: unknown }).group);
  }
  return null;
}

export function normalizeLayout(layout: unknown): TaskLayout {
  if (!layout || typeof layout !== "object") {
    return createDefaultLayout();
  }

  const rawLayout = layout as { root?: unknown; focusedPaneId?: unknown };
  const root = normalizeNode(rawLayout.root) ?? createDefaultLayout().root;
  const focusedPaneId = rawLayout.focusedPaneId === null ? null : trimNonEmpty(rawLayout.focusedPaneId);
  const resolvedFocusedPaneId =
    focusedPaneId === null
      ? null
      : ((focusedPaneId && findPaneById(root, focusedPaneId)?.id) ??
        collectAllPanes(root)[0]?.id ??
        DEFAULT_PANE_ID);

  return { root, focusedPaneId: resolvedFocusedPaneId };
}

export function findPaneById(root: SplitNode, paneId: string | null | undefined): SplitPane | null {
  if (!paneId) {
    return null;
  }
  if (root.kind === "pane") {
    return root.pane.id === paneId ? root.pane : null;
  }
  for (const child of root.group.children) {
    const pane = findPaneById(child, paneId);
    if (pane) {
      return pane;
    }
  }
  return null;
}

export function findPaneContainingTab(root: SplitNode, tabKey: string): SplitPane | null {
  if (root.kind === "pane") {
    return root.pane.tabs.some((tab) => tab.key === tabKey) ? root.pane : null;
  }
  for (const child of root.group.children) {
    const pane = findPaneContainingTab(child, tabKey);
    if (pane) {
      return pane;
    }
  }
  return null;
}

export function getTreeDepth(node: SplitNode): number {
  if (node.kind === "pane") {
    return 1;
  }
  return 1 + Math.max(...node.group.children.map((child) => getTreeDepth(child)));
}

export function collectAllTabs(root: SplitNode): TabEntry[] {
  if (root.kind === "pane") {
    return root.pane.tabs.slice();
  }
  return root.group.children.flatMap((child) => collectAllTabs(child));
}

export function collectAllPanes(root: SplitNode): SplitPane[] {
  if (root.kind === "pane") {
    return [root.pane];
  }
  return root.group.children.flatMap((child) => collectAllPanes(child));
}

export function createDefaultLayout(): TaskLayout {
  return {
    root: createPaneNode({ id: DEFAULT_PANE_ID, tabs: [], activeKey: null }),
    focusedPaneId: DEFAULT_PANE_ID,
  };
}

export function insertSplit(
  root: SplitNode,
  targetPaneId: string,
  tabKey: string,
  position: "left" | "right" | "top" | "bottom",
  createNodeId: (prefix: "pane" | "group") => string,
): SplitNode {
  return insertSplitInternal({ root, targetPaneId, tabKey, position, createNodeId }).root;
}

export function removePaneFromTree(root: SplitNode, paneId: string): SplitNode {
  const panePath = findPanePathById(root, paneId);
  if (!panePath) {
    return root;
  }
  return removePaneByPath(root, panePath);
}

export function removeTabFromTree(root: SplitNode, tabKey: string): SplitNode {
  return detachTabFromTree(root, { tabKey, preserveEmptyPaneId: DEFAULT_PANE_ID }).root;
}

function isLastVisibleOrdinaryPane(layout: TaskLayout, paneId: string): boolean {
  const panes = collectAllPanes(layout.root);
  return panes.length === 1 && panes[0]!.id === paneId;
}

/**
 * Whether dismissing this pane would do anything -- a workspace always has
 * somewhere to look, so the last remaining pane stays.
 */
export function canDismissPaneInLayout(layout: TaskLayout, paneId: string): boolean {
  const pane = findPaneById(layout.root, paneId);
  if (!pane) {
    return false;
  }
  return !isLastVisibleOrdinaryPane(layout, paneId);
}

/** Removes a pane outright, tabs and all. The last remaining pane never goes. */
export function closePaneInLayout(input: { layout: TaskLayout; paneId: string }): TaskLayout | null {
  const layout = input.layout;
  const panePath = findPanePathById(layout.root, input.paneId);
  if (!panePath) {
    return null;
  }
  if (isLastVisibleOrdinaryPane(layout, input.paneId)) {
    return null;
  }

  const fallbackPaneId = findNearestSiblingPaneId(layout.root, input.paneId);
  const nextRoot = removePaneByPath(layout.root, panePath);
  const nextFocusedPaneId =
    layout.focusedPaneId === input.paneId
      ? fallbackPaneId
      : (findPaneById(nextRoot, layout.focusedPaneId)?.id ?? fallbackPaneId);

  return { root: nextRoot, focusedPaneId: nextFocusedPaneId };
}

export function focusTabInLayout(input: { layout: TaskLayout; tabKey: string }): TaskLayout | null {
  const layout = input.layout;
  const pane = findPaneContainingTab(layout.root, input.tabKey);
  if (!pane) {
    return null;
  }
  if (pane.activeKey === input.tabKey && layout.focusedPaneId === pane.id) {
    return null;
  }
  return {
    root: focusTabInPane(layout.root, pane.id, input.tabKey),
    focusedPaneId: pane.id,
  };
}

export function splitPaneInLayout(input: {
  layout: TaskLayout;
  tabKey: string;
  targetPaneId: string;
  position: "left" | "right" | "top" | "bottom";
  createNodeId: (prefix: "pane" | "group") => string;
  maxTreeDepth: number;
}): { layout: TaskLayout; paneId: string } | null {
  const layout = input.layout;
  if (!findPaneById(layout.root, input.targetPaneId)) {
    return null;
  }
  if (!findPaneContainingTab(layout.root, input.tabKey)) {
    return null;
  }

  const result = insertSplitInternal({
    root: layout.root,
    targetPaneId: input.targetPaneId,
    tabKey: input.tabKey,
    position: input.position,
    createNodeId: input.createNodeId,
  });
  if (getTreeDepth(result.root) > input.maxTreeDepth) {
    return null;
  }

  return {
    paneId: result.newPaneId,
    layout: { root: result.root, focusedPaneId: result.newPaneId },
  };
}

export function splitPaneEmptyInLayout(input: {
  layout: TaskLayout;
  targetPaneId: string;
  position: "left" | "right" | "top" | "bottom";
  createNodeId: (prefix: "pane" | "group") => string;
  maxTreeDepth: number;
}): { layout: TaskLayout; paneId: string } | null {
  const layout = input.layout;
  if (!findPaneById(layout.root, input.targetPaneId)) {
    return null;
  }

  const direction = input.position === "left" || input.position === "right" ? "horizontal" : "vertical";
  const insertAfter = input.position === "right" || input.position === "bottom";

  const targetPath = findPanePathById(layout.root, input.targetPaneId);
  assert(targetPath !== null, `Target pane not found: ${input.targetPaneId}`);
  const targetNode = getNodeAtPath(layout.root, targetPath);
  assert(targetNode.kind === "pane", "Expected target pane");

  const newPaneId = input.createNodeId("pane");
  const newPaneNode = createPaneNode({ id: newPaneId, tabs: [], activeKey: null });

  const parentPath = targetPath.slice(0, -1);
  const targetIndex = targetPath[targetPath.length - 1] ?? 0;
  const parentNode = findParentGroup(layout.root, targetPath);

  let nextRoot: SplitNode;
  if (parentNode?.kind === "group" && parentNode.group.direction === direction) {
    const targetSize = parentNode.group.sizes[targetIndex] ?? 0;
    const nextSizes = parentNode.group.sizes.slice();
    const insertIndex = insertAfter ? targetIndex + 1 : targetIndex;
    nextSizes.splice(insertIndex, 0, targetSize / 2);
    nextSizes[targetIndex + (insertAfter ? 0 : 1)] = targetSize / 2;
    nextRoot = replaceNodeAtPath(layout.root, parentPath, () =>
      insertChildIntoGroup(parentNode, { index: insertIndex, node: newPaneNode, sizes: nextSizes }),
    );
  } else {
    const newGroup = createGroupNode({
      id: input.createNodeId("group"),
      direction,
      children: insertAfter ? [targetNode, newPaneNode] : [newPaneNode, targetNode],
      sizes: [0.5, 0.5],
    });
    nextRoot = replaceNodeAtPath(layout.root, targetPath, () => newGroup);
  }

  if (getTreeDepth(nextRoot) > input.maxTreeDepth) {
    return null;
  }

  return {
    paneId: newPaneId,
    layout: { root: nextRoot, focusedPaneId: newPaneId },
  };
}

export function moveTabToPaneInLayout(input: {
  layout: TaskLayout;
  tabKey: string;
  toPaneId: string;
}): TaskLayout | null {
  const layout = input.layout;
  const sourcePane = findPaneContainingTab(layout.root, input.tabKey);
  const targetPane = findPaneById(layout.root, input.toPaneId);
  if (!sourcePane || !targetPane) {
    return null;
  }

  const detached = detachTabFromTree(layout.root, {
    tabKey: input.tabKey,
    preserveEmptyPaneId: sourcePane.id === input.toPaneId ? sourcePane.id : null,
  });
  if (!detached.tab) {
    return null;
  }

  return {
    root: insertTabIntoPane(detached.root, {
      paneId: input.toPaneId,
      tab: detached.tab,
      focusTabKey: input.tabKey,
    }),
    focusedPaneId: input.toPaneId,
  };
}

export function focusPaneInLayout(input: { layout: TaskLayout; paneId: string }): TaskLayout | null {
  const pane = findPaneById(input.layout.root, input.paneId);
  if (!pane) {
    return null;
  }
  if (input.layout.focusedPaneId === input.paneId) {
    return null;
  }
  return { root: input.layout.root, focusedPaneId: input.paneId };
}

export function resizeSplitInLayout(input: { layout: TaskLayout; groupId: string; sizes: number[] }): TaskLayout {
  const layout = input.layout;
  return {
    root: updateGroupSizesInTree(layout.root, { groupId: input.groupId, sizes: input.sizes }),
    focusedPaneId: layout.focusedPaneId,
  };
}
