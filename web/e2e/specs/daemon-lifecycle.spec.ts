import fs from 'node:fs'
import { expect, test } from '@playwright/test'
import { startDaemon } from '../fixtures/daemon'
import { openRpc } from '../fixtures/rpc'
import { seedDaemon } from '../fixtures/seed'

test('fixture_stop_leaves_no_process_and_no_dir', async () => {
  const d = await startDaemon()
  const pid = d.pid()!
  const home = d.home
  const rpc = await openRpc(d.url)
  try {
    await seedDaemon(d)
    const t = await rpc.call('task.create', { workspaceId: d.ids.workspaces.alpha, title: 'lifecycle' })
    expect(fs.existsSync(t.WorktreePath)).toBe(true)
  } finally {
    rpc.close()
  }
  await d.stop()
  expect(() => process.kill(pid, 0)).toThrow()
  expect(fs.existsSync(home)).toBe(false)
})
