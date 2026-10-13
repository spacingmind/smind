/** The browser tab title with an unread-count prefix (AC2), e.g. "(3) sMind" -- unchanged when there's nothing unread. */
export function formatTabTitle(baseTitle: string, unreadCount: number): string {
  return unreadCount > 0 ? `(${unreadCount}) ${baseTitle}` : baseTitle;
}
