import { expect, type App } from './test'
import { openRpc } from './rpc'

/**
 * Opens a task's chat by deep link. Selecting through the sidebar row leaves the pointer on it, and its hover card can stay open
 * over the composer controls for the whole test; a deep link never hovers the sidebar.
 */
export async function openTaskByLink(app: App, task: { id: number }, title: string, workspace: 'alpha' | 'beta' | 'gamma' = 'alpha'): Promise<void> {
  const { page } = app
  const rpc = await openRpc(app.daemon.url)
  let chatId: number
  try {
    chatId = (await rpc.call('chat.list', { taskId: task.id }))[0].ID
  } finally {
    rpc.close()
  }
  await app.openApp(`#/workspace/${app.daemon.ids.workspaces[workspace]}/task/${task.id}/chat/${chatId}`)
  await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()
  await expect(page.locator('[aria-label="Prompt"]').first()).toBeVisible()
}
