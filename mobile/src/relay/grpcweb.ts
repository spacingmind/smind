// grpcweb.ts is a minimal, hand-rolled grpc-web wire client: unary calls
// over plain HTTPS POST (Admit/AdmitChallenge) using React Native's global
// fetch, and one bidi-streaming OpenData call over the websocket transport
// internal/relay/server/grpcweb.go's WithWebsockets(true) turns on, using
// React Native's global WebSocket.
//
// No @improbable-eng/grpc-web or protoc-generated client here, for the
// same reason internal/relay/server/grpcweb_test.go hand-rolls its Go-side
// test client: the message set (AdmitChallenge, Admit, Frame) is small
// and fixed, and @improbable-eng/grpc-web's generated-stub abstraction
// would need a protoc/protoc-gen-grpc-web toolchain wired into mobile/'s
// build for five flat messages. Its `WebsocketTransport()` (reverse-
// engineered from its published source while building this) confirmed the
// exact wire format implemented here is correct: subprotocol
// "grpc-websockets", one leading control byte per client->server message,
// and the relay's own header/trailer framing on responses.

// --- Unary calls (Admit / AdmitChallenge) ---

interface GRPCWebTrailers {
  status: string;
  message: string;
}

function frameLPM(payload: Uint8Array) {
  const frame = new Uint8Array(5 + payload.length);
  new DataView(frame.buffer).setUint32(1, payload.length, false);
  frame.set(payload, 5);
  return frame;
}

/** Walks the concatenated data+trailer frames a grpc-web unary response body carries. */
function parseGRPCWebFrames(data: Uint8Array): { messages: Uint8Array[]; trailers: GRPCWebTrailers } {
  const messages: Uint8Array[] = [];
  let trailers: GRPCWebTrailers = { status: '', message: '' };
  let pos = 0;
  while (pos < data.length) {
    if (data.length - pos < 5) throw new Error('grpc-web: truncated frame header');
    const flag = data[pos];
    const length = new DataView(data.buffer, data.byteOffset + pos + 1, 4).getUint32(0, false);
    pos += 5;
    if (data.length - pos < length) throw new Error('grpc-web: truncated frame body');
    const payload = data.slice(pos, pos + length);
    pos += length;
    if (flag & 0x80) {
      trailers = parseTrailerPayload(payload);
    } else {
      messages.push(payload);
    }
  }
  return { messages, trailers };
}

function parseTrailerPayload(payload: Uint8Array): GRPCWebTrailers {
  const text = new TextDecoder().decode(payload);
  let status = '';
  let message = '';
  for (const line of text.split('\r\n')) {
    const idx = line.indexOf(':');
    if (idx < 0) continue;
    const key = line.slice(0, idx).trim().toLowerCase();
    const value = line.slice(idx + 1).trim();
    if (key === 'grpc-status') status = value;
    if (key === 'grpc-message') message = value;
  }
  return { status, message };
}

/** Completes one unary grpc-web RPC (Admit, AdmitChallenge) over plain HTTPS POST. */
export async function grpcWebUnary<Resp>(baseUrl: string, method: string, requestBytes: Uint8Array, decode: (b: Uint8Array) => Resp): Promise<Resp> {
  const resp = await fetch(`${baseUrl}/relay.v1.Relay/${method}`, {
    method: 'POST',
    // x-grpc-web: 1 is the protocol's documented client indicator header.
    // It matters in browsers: the relay's grpc-web wrapper (traefik/grpc-web)
    // only routes a CORS preflight through its permissive CORS handler when
    // the request advertises that header, so omitting it makes the browser
    // fetch fail the preflight with no Access-Control-Allow-Origin. Node/RN
    // fetch never preflights, which is why the integration tests pass either
    // way -- a real browser target (the web smoke test) needs it.
    headers: { 'content-type': 'application/grpc-web+proto', 'x-grpc-web': '1' },
    body: frameLPM(requestBytes),
  });
  const buf = new Uint8Array(await resp.arrayBuffer());
  const { messages, trailers } = parseGRPCWebFrames(buf);
  if (trailers.status && trailers.status !== '0') {
    throw new Error(`grpc-web ${method}: status ${trailers.status}: ${trailers.message}`);
  }
  if (messages.length === 0) {
    throw new Error(`grpc-web ${method}: no response message (status ${trailers.status || 'unknown'})`);
  }
  return decode(messages[0]);
}

// --- Bidi streaming (OpenData) over the websocket transport ---

/**
 * GRPCWebSocketStream drives one bidi-streaming RPC (OpenData) over
 * grpc-web's websocket transport: dial with the "grpc-websockets"
 * subprotocol, send one leading binary frame with the gRPC metadata as
 * raw header text, then exchange length-prefixed protobuf messages, each
 * client->server one additionally prefixed with a control byte (0 = more
 * data). Server->client messages carry no such control byte; the very
 * first one is always a header frame (flag 0x80), and the reassembly here
 * must not assume header/payload land in the same websocket message --
 * grpc-go's HTTP/2-via-http.Handler write path issues separate Write()
 * calls for a frame's header and its payload, which arrive as two
 * distinct websocket messages.
 */
export class GRPCWebSocketStream {
  private ws: WebSocket;
  private pending = new Uint8Array(0);
  private messageResolvers: Array<() => void> = [];
  private closedError: Error | null = null;
  private opened: Promise<void>;

  constructor(wsUrl: string, headers: Record<string, string>) {
    this.ws = new WebSocket(wsUrl, ['grpc-websockets']);
    this.ws.binaryType = 'arraybuffer';

    this.opened = new Promise((resolve, reject) => {
      this.ws.onopen = () => {
        let headerText = 'content-type: application/grpc-web+proto\r\n';
        for (const [k, v] of Object.entries(headers)) headerText += `${k}: ${v}\r\n`;
        this.ws.send(new TextEncoder().encode(headerText));
        resolve();
      };
      this.ws.onerror = () => reject(new Error('grpc-web websocket: connection error'));
    });

    this.ws.onmessage = (event: MessageEvent) => {
      const data = event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : new Uint8Array(0);
      const combined = new Uint8Array(this.pending.length + data.length);
      combined.set(this.pending, 0);
      combined.set(data, this.pending.length);
      this.pending = combined;
      const resolver = this.messageResolvers.shift();
      if (resolver) resolver();
    };
    this.ws.onclose = () => {
      if (!this.closedError) this.closedError = new Error('grpc-web websocket: closed');
      while (this.messageResolvers.length > 0) this.messageResolvers.shift()!();
    };
  }

  async ready(): Promise<void> {
    await this.opened;
  }

  /** Sends one protobuf-encoded message as a client->server frame. */
  send(messageBytes: Uint8Array): void {
    const lpm = frameLPM(messageBytes);
    const frame = new Uint8Array(1 + lpm.length);
    frame[0] = 0x00; // more data follows.
    frame.set(lpm, 1);
    this.ws.send(frame);
  }

  /** Reassembles and returns exactly one flag+payload record, waiting for more websocket messages as needed. */
  private async readRecord(): Promise<{ flag: number; payload: Uint8Array }> {
    for (;;) {
      if (this.pending.length >= 5) {
        const flag = this.pending[0];
        const length = new DataView(this.pending.buffer, this.pending.byteOffset + 1, 4).getUint32(0, false);
        if (this.pending.length >= 5 + length) {
          const payload = this.pending.slice(5, 5 + length);
          this.pending = this.pending.slice(5 + length);
          return { flag, payload };
        }
      }
      if (this.closedError) throw this.closedError;
      await new Promise<void>((resolve) => this.messageResolvers.push(resolve));
    }
  }

  /**
   * Decodes exactly one application-level message (a relay.v1.Frame),
   * skipping the header frame grpc-web always sends first and surfacing a
   * trailer (an RPC error) as a thrown error.
   */
  async receiveMessage<Msg>(decode: (b: Uint8Array) => Msg): Promise<Msg> {
    for (;;) {
      const { flag, payload } = await this.readRecord();
      if (flag & 0x80) {
        const trailers = parseTrailerPayload(payload);
        if (trailers.status && trailers.status !== '0') {
          throw new Error(`grpc-web OpenData: status ${trailers.status}: ${trailers.message}`);
        }
        continue; // it was the initial headers frame.
      }
      return decode(payload);
    }
  }

  close(): void {
    this.ws.close();
  }
}
