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
  // nothing outliving the daemon may re-create the home afterwards: it stays absent for 1.5 s
  const t0 = Date.now()
  let recreated = false
  await expect
    .poll(
      () => {
        if (fs.existsSync(home)) recreated = true
        return recreated ? 'recreated' : Date.now() - t0 >= 1_500 ? 'absent' : 'waiting'
      },
      { timeout: 5_000, intervals: [100] },
    )
    .not.toBe('waiting')
  expect(recreated).toBe(false)
})
