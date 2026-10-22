import { create } from "zustand";

/**
 * Which of a task's chats is "in front" right now (not persisted): App.tsx derives it from the tab layout and
 * writes it here, so a task-level surface that has no chat of its own -- the Diff pane's "Submit review" -- can
 * address the chat the user is actually working in without a prop chain through the split tree.
 */
export const useFocusedChatStore = create<{
  byTask: Readonly<Record<number, number>>;
  setFocusedChat: (taskId: number, chatId: number | null) => void;
}>()((set) => ({
  byTask: {},
  setFocusedChat: (taskId, chatId) =>
    set((state) => {
      if ((state.byTask[taskId] ?? null) === chatId) return state;
      const next = { ...state.byTask };
      if (chatId === null) delete next[taskId];
      else next[taskId] = chatId;
      return { byTask: next };
    }),
}));

/** The focused chat id of taskId, or null when none is open. */
export function useFocusedChatId(taskId: number): number | null {
  return useFocusedChatStore((state) => state.byTask[taskId] ?? null);
}
