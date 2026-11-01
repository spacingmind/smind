import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import { MARKER, assertSafeHome, assertSafePort, getFreePort } from '../fixtures/guard'

test('fixture_refuses_foreign_home', () => {
  expect(() => assertSafeHome(path.join(os.homedir(), '.smind'))).toThrow(/refusing home/)
  expect(() => assertSafeHome('/tmp/x')).toThrow(/refusing home/)
  expect(() => assertSafeHome(os.tmpdir())).toThrow(/refusing home/)
  const bare = fs.mkdtempSync(path.join(os.tmpdir(), 'smind-e2e-guard-'))
  try {
    expect(() => assertSafeHome(bare)).toThrow(/marker/)
    fs.writeFileSync(path.join(bare, MARKER), '')
    expect(() => assertSafeHome(bare)).not.toThrow()
  } finally {
    fs.rmSync(bare, { recursive: true, force: true })
  }
})

test('fixture_refuses_reserved_port', async () => {
  expect(() => assertSafePort(4648)).toThrow(/reserved/)
  expect(() => assertSafePort(4870)).toThrow(/reserved/)
  expect(() => assertSafePort(5123)).toThrow(/not allocated/)
  const port = await getFreePort()
  expect(() => assertSafePort(port)).not.toThrow()
})
