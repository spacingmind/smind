// Scripted ACP fake agent for the e2e suite (copied from the phase 1 UAT harness, body unchanged).
// Scenarios: default, hang, slow, permission, error, long; read from the `scenario` file in the session cwd.
// Scripted ACP agent for UAT (no network, no model). Scenario = trimmed contents of the "scenario" file in the session cwd,
// read at each prompt: default | hang | slow | permission | error | long.
const fs = require('fs'), path = require('path');
const rl = require('readline').createInterface({ input: process.stdin });
const out = (m) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...m }) + '\n');
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sid = 'uat-1';
let cwd = process.cwd();
let cancelled = false, wake = null, nextId = 1000;
const pendingCalls = new Map();
const up = (u) => out({ method: 'session/update', params: { sessionId: sid, update: u } });
const txt = (t) => ({ sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: t } });
const scenario = () => { try { return fs.readFileSync(path.join(cwd, 'scenario'), 'utf8').trim() || 'default'; } catch { return 'default'; } };
// Sleep that returns early (true) when the turn is cancelled.
const cancellable = (ms) => new Promise((r) => { const t = ms == null ? null : setTimeout(() => { wake = null; r(false); }, ms); wake = () => { if (t) clearTimeout(t); wake = null; r(true); }; });
const call = (method, params) => new Promise((resolve) => { const id = nextId++; pendingCalls.set(id, resolve); out({ id, method, params }); });

async function rich() {
  up({ sessionUpdate: 'agent_thought_chunk', content: { type: 'text', text: 'Looking at the router first, then the failing test.' } });
  await sleep(150);
  up({ sessionUpdate: 'plan', entries: [{ content: 'Read the routing table', status: 'completed', priority: 'high' }, { content: 'Fix the retry backoff', status: 'in_progress', priority: 'high' }, { content: 'Run the tests', status: 'pending', priority: 'medium' }] });
  up({ sessionUpdate: 'tool_call', toolCallId: 't1', title: 'Read src/routing/router.go', kind: 'read', status: 'completed', locations: [{ path: 'src/routing/router.go' }], content: [{ type: 'content', content: { type: 'text', text: 'package routing\n\nfunc Pick(...) {}' } }] });
  up({ sessionUpdate: 'tool_call', toolCallId: 't2', title: 'grep -rn "backoff" src/', kind: 'search', status: 'completed', content: [{ type: 'content', content: { type: 'text', text: 'src/routing/retry.go:12: backoff := time.Second' } }] });
  up({ sessionUpdate: 'tool_call', toolCallId: 't3', title: 'Edit src/routing/retry.go', kind: 'edit', status: 'completed', locations: [{ path: 'src/routing/retry.go' }], content: [{ type: 'diff', path: 'src/routing/retry.go', oldText: 'backoff := time.Second\n', newText: 'backoff := time.Second << attempt\n' }] });
  up({ sessionUpdate: 'tool_call', toolCallId: 't4', title: 'go test ./src/routing/...', kind: 'execute', status: 'completed', content: [{ type: 'content', content: { type: 'text', text: 'ok  \tuat/src/routing\t0.412s' } }] });
  up({ sessionUpdate: 'tool_call', toolCallId: 't5', title: 'go vet ./...', kind: 'execute', status: 'failed', content: [{ type: 'content', content: { type: 'text', text: '# uat/src/routing\nretry.go:14:2: declared and not used: n' } }] });
  await sleep(150);
  for (const c of ['I fixed the **retry backoff** so it grows exponentially.\n\n', '## What changed\n\n', '- `retry.go`: shift by `attempt`\n- tests pass\n\n', '```go\nbackoff := time.Second << attempt\n```\n\n', '### Result table\n\n| file | change |\n| --- | --- |\n| `retry.go` | exponential backoff |\n| `router.go` | none |\n\nSee [the docs](https://example.com/retry) for details.\n\n', '`go vet` still reports one unused variable; want me to remove it?']) { up(txt(c)); await sleep(40); }
}

async function prompt(m) {
  cancelled = false;
  const sc = scenario();
  const done = (stopReason = 'end_turn') => out({ id: m.id, result: { stopReason } });
  switch (sc) {
    case 'hang':
      up(txt('Working on it... '));
      await cancellable(null);
      return done('cancelled');
    case 'slow':
      for (let i = 1; i <= 5; i++) { up(txt(`Chunk ${i} of 5. `)); if (await cancellable(300)) return done('cancelled'); }
      return done();
    case 'error':
      up(txt('About to fail... '));
      return out({ id: m.id, error: { code: -32603, message: 'uat-agent: simulated provider failure (scenario error)' } });
    case 'long': {
      up({ sessionUpdate: 'agent_thought_chunk', content: { type: 'text', text: 'Producing a long answer for scroll tests.' } });
      let s = '# Long answer\n\n';
      for (let i = 1; i <= 400; i++) {
        s += i % 40 === 1 ? `\n## Section ${Math.ceil(i / 40)}\n\n` : '';
        s += `${i}. Line ${i}: the quick brown fox jumps over the lazy dog, with \`code_${i}\` and **bold ${i}**.\n`;
        if (i % 20 === 0) { up(txt(s)); s = ''; await sleep(5); }
      }
      return done();
    }
    case 'permission': {
      up(txt('I need to edit a file. '));
      const r = await call('session/request_permission', {
        sessionId: sid,
        toolCall: { toolCallId: 'p1', title: 'Edit src/app.go', kind: 'edit', status: 'pending', locations: [{ path: 'src/app.go' }], content: [{ type: 'diff', path: 'src/app.go', oldText: 'x := 1\n', newText: 'x := 2\n' }] },
        options: [{ optionId: 'allow_once', name: 'Allow once', kind: 'allow_once' }, { optionId: 'allow_always', name: 'Allow always', kind: 'allow_always' }, { optionId: 'reject_once', name: 'Reject', kind: 'reject_once' }],
      });
      const o = r && r.outcome;
      if (!o || o.outcome === 'cancelled') return done('cancelled');
      up(txt(`Permission answer: ${o.optionId}. `));
      if (o.optionId && o.optionId.startsWith('allow')) up({ sessionUpdate: 'tool_call', toolCallId: 'p1', title: 'Edit src/app.go', kind: 'edit', status: 'completed', content: [{ type: 'diff', path: 'src/app.go', oldText: 'x := 1\n', newText: 'x := 2\n' }] });
      up(txt('Done.'));
      return done();
    }
    default:
      await rich();
      return done();
  }
}

rl.on('line', async (l) => {
  let m; try { m = JSON.parse(l); } catch { return; }
  if (m.method === undefined && m.id !== undefined && pendingCalls.has(m.id)) { const r = pendingCalls.get(m.id); pendingCalls.delete(m.id); return r(m.result || {}); }
  if (m.method === 'initialize') return out({ id: m.id, result: { protocolVersion: 1, agentCapabilities: { promptCapabilities: {} } } });
  if (m.method === 'session/new') { if (m.params && m.params.cwd) cwd = m.params.cwd; return out({ id: m.id, result: { sessionId: sid } }); }
  if (m.method === 'session/cancel') { cancelled = true; if (wake) wake(); for (const [id, r] of pendingCalls) { pendingCalls.delete(id); r({ outcome: { outcome: 'cancelled' } }); } return; }
  if (m.method === 'session/prompt') return prompt(m);
  if (m.id !== undefined && m.method) out({ id: m.id, result: {} });
});
