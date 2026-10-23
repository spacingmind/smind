// ADR-0038 mobile prompt attachments: the composer's Attach menu, the pending
// tray, inline refusals and daemon errors, and a sent prompt's pills in the
// timeline. The web pickers are <input type=file> elements: the stage answers
// their filechooser event with small generated files. Frames (light + dark):
// attach-composer-empty, attach-tray-image-pill, attach-tray-file-pill,
// attach-refusal-over-limit, attach-pill-added / attach-pill-removed,
// attach-daemon-error, attach-sent-prompt-timeline.

import { deflateSync } from 'node:zlib';
import * as lib from '../lib.mjs';

function crc32(buf) {
  let c;
  let crc = 0xffffffff;
  for (let n = 0; n < buf.length; n++) {
    c = (crc ^ buf[n]) & 0xff;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    crc = (crc >>> 8) ^ c;
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  out.write(type, 4, 'ascii');
  data.copy(out, 8);
  out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
  return out;
}

/** A size x size RGB PNG with a diagonal gradient. */
function makePng(size) {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // RGB
  const raw = Buffer.alloc(size * (1 + size * 3));
  for (let y = 0; y < size; y++) {
    const row = y * (1 + size * 3);
    for (let x = 0; x < size; x++) {
      raw[row + 1 + x * 3] = Math.floor((x / size) * 255);
      raw[row + 2 + x * 3] = Math.floor((y / size) * 255);
      raw[row + 3 + x * 3] = 160;
    }
  }
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

const PNG = { name: 'tiny.png', mimeType: 'image/png', buffer: makePng(64) };
const TEXT = { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('smoke notes\n') };
const BIG = { name: 'report.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(1_200_000, 'a') };
const NOT_AN_IMAGE = { name: 'fake.png', mimeType: 'image/png', buffer: Buffer.from('this is not a png at all') };

/** Attach button -> menu item -> answer the picker's file chooser. */
async function pick(page, item, files) {
  await page.getByRole('button', { name: 'Attach', exact: true }).click();
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser', { timeout: 10_000 }),
    page.getByRole('button', { name: item, exact: true }).click(),
  ]);
  await chooser.setFiles(files);
  // The menu sheet closes as the picker opens; wait for it to be gone before the caller types or sends.
  await page.getByRole('button', { name: item, exact: true }).waitFor({ state: 'detached', timeout: 10_000 }).catch(() => {});
}

const removeButton = (page, name) => page.getByRole('button', { name: `Remove ${name}`, exact: true });

async function expectVisible(page, locator, what) {
  try {
    await locator.waitFor({ timeout: 15_000 });
    return true;
  } catch {
    lib.fail(`${what} (page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))})`);
    return false;
  }
}

async function attachmentsFlow(page, harness) {
  const seeded = await harness.cmd('seed-run end_turn Attach task');
  if (!seeded.ok) {
    lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  if (!(await expectVisible(page, page.getByText('Attach task', { exact: true }), 'the seeded task did not appear on the task list'))) return;
  await page.getByText('Attach task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });

  // AC1: the Attach button shows (the harness daemon has an attachment store, so daemon.info lists kinds).
  if (!(await expectVisible(page, page.getByRole('button', { name: 'Attach', exact: true }), 'the Attach button is not shown'))) return;
  await lib.shootBothSchemes(page, 'attach-composer-empty');

  // Image pill.
  await pick(page, 'Photo library', [PNG]);
  if (!(await expectVisible(page, page.getByText('tiny.png', { exact: true }), 'the image pill did not appear'))) return;
  if (!(await removeButton(page, 'tiny.png').isVisible())) lib.fail('the image pill has no Remove button');
  await lib.shootBothSchemes(page, 'attach-tray-image-pill');

  // File pill.
  await pick(page, 'Files', [TEXT]);
  if (!(await expectVisible(page, page.getByText('notes.txt', { exact: true }), 'the file pill did not appear'))) return;
  if (!(await page.getByText('12 B', { exact: true }).isVisible().catch(() => false))) lib.fail('the file pill does not show its size (12 B)');
  await lib.shootBothSchemes(page, 'attach-tray-file-pill');
  await lib.shootBothSchemes(page, 'attach-pill-added');

  // Over the relay budget: refused inline, naming the file and the limit; nothing added.
  await pick(page, 'Files', [BIG]);
  const notice = page.getByText(/report\.bin/);
  if (await expectVisible(page, notice, 'no inline refusal for the over-limit file')) {
    const text = (await notice.first().innerText()).trim();
    if (!text.includes('report.bin') || !text.includes('780 KB')) lib.fail(`the refusal must name the file and 780 KB, got: ${JSON.stringify(text)}`);
  }
  if (await removeButton(page, 'report.bin').isVisible().catch(() => false)) lib.fail('the over-limit file was added to the tray');
  await lib.shootBothSchemes(page, 'attach-refusal-over-limit');

  // Remove works.
  await removeButton(page, 'notes.txt').click();
  try {
    await page.getByText('notes.txt', { exact: true }).waitFor({ state: 'detached', timeout: 10_000 });
  } catch {
    lib.fail('Remove did not take the file pill out of the tray');
  }
  await lib.shootBothSchemes(page, 'attach-pill-removed');

  // A daemon error: bytes that are not a PNG, declared image/png, pass the phone's checks (a declared png within 1568px is sent as is)
  // and are refused by the daemon (mime_mismatch); the draft and the tray stay.
  await pick(page, 'Photo library', [NOT_AN_IMAGE]);
  await page.getByText('fake.png', { exact: true }).waitFor({ timeout: 15_000 }).catch(() => {});
  if (await page.getByText('fake.png', { exact: true }).isVisible().catch(() => false)) {
    await page.locator('textarea').fill('with a bad image');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    // The daemon names the position only for some codes; a content-sniff failure arrives as "An attachment is an image type the daemon does not accept."
    const err = page.getByText(/image type the daemon does not accept|not the kind of image|could not be read/);
    if (await expectVisible(page, err.first(), 'the daemon attachment error is not shown inline')) {
      await lib.shootBothSchemes(page, 'attach-daemon-error');
    }
    if (!(await removeButton(page, 'fake.png').isVisible().catch(() => false))) lib.fail('a failed send dropped the tray');
    if ((await page.locator('textarea').inputValue()) !== 'with a bad image') lib.fail('a failed send dropped the draft');
    await removeButton(page, 'fake.png').click();
  } else {
    lib.fail('the non-image picked as an image did not reach the tray (picker re-encoded or refused it on the phone); the daemon-error frame needs another route');
  }

  // Send for real: the tray empties and the prompt's pills show under the user bubble.
  await pick(page, 'Files', [TEXT]);
  await page.getByText('notes.txt', { exact: true }).waitFor({ timeout: 15_000 });
  await page.locator('textarea').fill('look at these');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  if (!(await expectVisible(page, page.getByText('look at these', { exact: true }), 'the sent prompt did not show'))) return;
  try {
    await page.waitForFunction(() => document.querySelectorAll('[aria-label^="Remove "]').length === 0, null, { timeout: 20_000 });
  } catch {
    lib.fail('Send did not clear the tray');
  }
  if ((await page.getByText('notes.txt', { exact: true }).count()) < 1) lib.fail('the sent prompt shows no attachment pills in the timeline');
  if ((await page.getByText('tiny.png', { exact: true }).count()) < 1) lib.fail('the sent image pill is missing from the timeline');
  await page.waitForTimeout(500);
  await lib.shootBothSchemes(page, 'attach-sent-prompt-timeline');

  // Reopen the task: the timeline now builds from run.list's attachment metadata (no local uris), and the image's
  // thumbnail arrives lazily through run.attachment as a data: image; the file stays a pill.
  await lib.goBack(page);
  await page.getByLabel(/^Attach task, /).getByText('Attach task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  if (!(await expectVisible(page, page.getByText('notes.txt', { exact: true }), 'the reopened task shows no attachment pills'))) return;
  try {
    await page.waitForFunction(() => [...document.querySelectorAll('img')].some((i) => i.src.startsWith('data:image/png;base64,')), null, { timeout: 20_000 });
  } catch {
    lib.fail('the image attachment did not load a thumbnail through run.attachment');
  }
  await page.waitForTimeout(300);
  await lib.shootBothSchemes(page, 'attach-reloaded-thumbnail');
}

export default {
  name: 'attachments',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'attachments');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await attachmentsFlow(page, harness);
    await context.close();
  },
};
