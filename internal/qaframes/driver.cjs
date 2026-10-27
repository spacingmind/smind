// Embedded driver: reads one JSON job on stdin, drives ONE browser through every shot sequentially,
// writes one JSON event per line on stdout. Never installs anything. Closes the browser on exit/SIGTERM/SIGINT.
'use strict';
const fs = require('fs');
const path = require('path');
let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (d) => (input += d));
process.stdin.on('end', () => run(JSON.parse(input)).then(() => process.exit(0), (e) => { emit({ event: 'fatal', error: String(e && e.stack || e) }); process.exit(1); }));
const emit = (o) => process.stdout.write(JSON.stringify(o) + '\n');
let browser = null;
const bye = async () => { try { if (browser) await browser.close(); } catch (_) {} process.exit(143); };
process.on('SIGTERM', bye);
process.on('SIGINT', bye);

async function run(job) {
  const pw = require(path.join(job.playwrightDir, job.playwrightPkg));
  browser = await pw.chromium.launch({ executablePath: job.chrome });
  const proc = browser.process && browser.process();
  emit({ event: 'browser', pid: proc ? proc.pid : 0 });
  for (const shot of job.shots) {
    const res = { event: 'shot', key: shot.key, files: [], steps: [], error: '' };
    const ctx = await browser.newContext({ viewport: { width: shot.viewport[0], height: shot.viewport[1] }, colorScheme: shot.theme });
    const page = await ctx.newPage();
    seq = 0;
    fs.mkdirSync(shot.dir, { recursive: true });
    try {
      for (const st of shot.steps) await step(page, st, shot, res, job);
    } catch (e) {
      res.error = String(e && e.message || e);
    }
    emit(res);
    await ctx.close();
  }
  await browser.close();
  browser = null;
  emit({ event: 'done' });
}

let seq = 0;
async function snap(page, shot, res, name, t0) {
  const file = path.join(shot.dir, `${String(seq++).padStart(3, '0')}-${name}.png`);
  try {
    await page.screenshot({ path: file });
  } catch (_) {
    return; // mid-navigation frames can fail; the sheet shows the gap
  }
  res.files.push({ file, name, tMs: t0 === undefined ? 0 : Date.now() - t0 });
}

async function step(page, st, shot, res, job) {
  res.steps.push(describe(st));
  switch (st.do) {
    case 'goto': await page.goto(job.base + '/' + (st.route || shot.route)); break;
    case 'reload': await page.reload(); break;
    case 'wait': await page.waitForTimeout(st.ms || 500); break;
    case 'waitFor': await page.waitForSelector(st.sel, { timeout: st.timeoutMs || 10000 }); break;
    case 'click': await page.click(st.sel); break;
    case 'clickText': await page.getByText(st.text, { exact: !!st.exact }).first().click(); break;
    case 'fill': await page.fill(st.sel || 'textarea', st.text); break;
    case 'type': await page.keyboard.type(st.text); break;
    case 'press': await page.keyboard.press(st.key); break;
    case 'expectText': {
      const body = await page.locator('body').innerText();
      if (!body.includes(st.text)) throw new Error('expected text not on page: ' + st.text);
      break;
    }
    case 'shot': await snap(page, shot, res, st.name || 'shot'); break;
    case 'burst': {
      // timed screenshots at a fixed step while an action runs: the transition frames
      const n = Math.min(st.frames || 12, 12), gap = st.gapMs || 80, t0 = Date.now();
      const action = st.then ? step(page, st.then, shot, res, job).catch(() => {}) : Promise.resolve();
      for (let i = 0; i < n; i++) {
        await snap(page, shot, res, `${st.name || 'burst'}-t${String(i).padStart(2, '0')}`, t0);
        await page.waitForTimeout(gap);
      }
      await action;
      break;
    }
    case 'send': {
      // the Go side already refused unless sends are allowed and the model is pinned; check again here
      if (!job.allowSend || job.model !== 'claude-haiku-5-5') throw new Error('send refused: not allowed or model not haiku');
      await page.fill(st.sel || 'textarea', st.text);
      await page.click(st.sendSel || '[aria-label="Send now"], [aria-label="Send"]');
      break;
    }
    default: throw new Error('unknown step: ' + st.do);
  }
}
function describe(st) { return st.do + (st.sel ? ' ' + st.sel : st.text ? ' ' + JSON.stringify(st.text).slice(0, 40) : st.route ? ' ' + st.route : ''); }
