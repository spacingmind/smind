// Evaluates a JavaScript expression in the running desktop app's webview over the
// Chrome DevTools Protocol and prints the JSON result. The app is started with
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=<port>; the
// expression is typically a Tauri command call:
//
//   node cdp-invoke.mjs 9222 'window.__TAURI_INTERNALS__.invoke("daemon_status")'
//
// Exit codes: 0 ok (result on stdout), 1 the expression threw (message on stderr),
// 2 could not reach the webview.
import http from "node:http";

const [port, expression, timeoutArg] = process.argv.slice(2);
if (!port || !expression) {
  console.error("usage: node cdp-invoke.mjs <port> <expression> [timeout-seconds]");
  process.exit(2);
}
const deadline = Date.now() + Number(timeoutArg ?? 60) * 1000;

function getJson(url) {
  return new Promise((resolve, reject) => {
    http
      .get(url, (res) => {
        let body = "";
        res.on("data", (c) => (body += c));
        res.on("end", () => {
          try {
            resolve(JSON.parse(body));
          } catch (e) {
            reject(e);
          }
        });
      })
      .on("error", reject);
  });
}

async function findPage() {
  for (;;) {
    try {
      const targets = await getJson(`http://127.0.0.1:${port}/json`);
      const page = targets.find((t) => t.type === "page" && t.webSocketDebuggerUrl);
      if (page) return page;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) {
      console.error(`no webview page on port ${port} within the timeout`);
      process.exit(2);
    }
    await new Promise((r) => setTimeout(r, 500));
  }
}

const page = await findPage();
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
  ws.onopen = resolve;
  ws.onerror = () => reject(new Error("websocket error"));
});
let nextId = 0;
const pending = new Map();
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.id && pending.has(msg.id)) {
    pending.get(msg.id)(msg);
    pending.delete(msg.id);
  }
};
const send = (method, params) =>
  new Promise((resolve) => {
    const id = ++nextId;
    pending.set(id, resolve);
    ws.send(JSON.stringify({ id, method, params }));
  });

// The page may still be loading: wait for the Tauri bridge to exist.
for (;;) {
  const probe = await send("Runtime.evaluate", { expression: "typeof window.__TAURI_INTERNALS__", returnByValue: true });
  if (probe.result?.result?.value === "object") break;
  if (Date.now() > deadline) {
    console.error("the page has no Tauri bridge (window.__TAURI_INTERNALS__) within the timeout");
    process.exit(2);
  }
  await new Promise((r) => setTimeout(r, 500));
}

const reply = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
ws.close();
if (reply.result?.exceptionDetails) {
  const d = reply.result.exceptionDetails;
  console.error(d.exception?.value ?? d.exception?.description ?? d.text);
  process.exit(1);
}
console.log(JSON.stringify(reply.result?.result?.value));
