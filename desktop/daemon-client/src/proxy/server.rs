//! The loopback proxy itself (AC2): an axum HTTP server that serves the
//! bundled UI as static assets (with SPA fallback), reverse-proxies
//! `/api/*` (HTTP, streamed both ways) and `/ws` (WebSocket, bridged
//! frame-by-frame) to the currently selected connection, dropping the
//! inbound `Origin` header on every proxied request -- and gates every
//! request (assets, `/api/*`, `/ws` alike) behind the per-launch secret
//! cookie from `crate::proxy::secret`.
//!
//! Kept in this GUI-free crate (like the rest of daemon-client, see
//! ADR-0012) so the integration test below can start a real proxy
//! against a stub HTTP+WS server without webkit2gtk.

use std::sync::{Arc, Mutex};

use axum::body::Body;
use axum::extract::ws::{Message as AxumMessage, WebSocket, WebSocketUpgrade};
use axum::extract::{RawQuery, State};
use axum::http::{header, HeaderMap, HeaderValue, Request, StatusCode, Uri};
use axum::middleware::{self, Next};
use axum::response::{IntoResponse, Redirect, Response};
use axum::routing::{any, get};
use axum::Router;
use futures_util::{SinkExt, StreamExt};
use tokio::net::TcpListener;
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::Message as TMessage;
use url::Url;

use crate::proxy::connections::{Connection, ConnectionKind, Registry, LOCAL_ID};
use crate::proxy::secret::{self, GateOutcome};
use crate::relay::client::RelayHandle;

/// AssetSource serves the bundled UI's static files. Implemented by a
/// `rust-embed`-backed type in `desktop/src-tauri` for the real app, and
/// by an in-memory map here for tests -- this crate itself never needs
/// to know how the assets got embedded.
pub trait AssetSource: Send + Sync + 'static {
    /// get returns `(bytes, content_type)` for `path` (no leading
    /// slash), or None if there's no such asset.
    fn get(&self, path: &str) -> Option<(Vec<u8>, String)>;
}

const GATE_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(3);

pub struct ProxyState {
    pub secret: String,
    pub registry: std::sync::Arc<Mutex<Registry>>,
    pub assets: Box<dyn AssetSource>,
    pub http_client: reqwest::Client,
    /// The relay transport for the currently-selected connection, if it
    /// is `relay`-kind -- `None` for `local`/`url` connections, and
    /// while a relay connection is still (re)connecting for the first
    /// time. Shared (not owned) with `client_watch` (AC7): both the
    /// `/ws` bridge below and the daemon-client watcher read/write the
    /// same underlying tunnel via cloned `RelayHandle`s, matching the
    /// plan's "one relay connection is one shared, multiplexed pipe"
    /// decision.
    pub relay: Mutex<Option<RelayHandle>>,
    /// ADR-0031 D8: per-connection session tokens from a password login,
    /// keyed by connection id. Memory only -- never persisted, and never
    /// sent to the webview (the proxy attaches them upstream itself).
    sessions: Mutex<std::collections::HashMap<String, String>>,
    /// Where the `local` connection's daemon token comes from (the token
    /// file by default; swapped in tests).
    local_token_source: Mutex<Arc<dyn Fn() -> Option<String> + Send + Sync>>,
    /// Whether the `local` daemon at a base URL answered `GET /api/token`
    /// with 403 (a password-protected daemon, ADR-0031 D8), learned from the
    /// UI's own token fetch. Only then does the proxy authenticate with the
    /// token file; an older daemon keeps today's token-query flow.
    local_password: Mutex<Option<(String, bool)>>,
    /// Short-timeout, no-redirect client for gate decisions (`/api/token` and
    /// `/healthz` probes) -- never the streaming `http_client`.
    gate_client: reqwest::Client,
    /// ADR-0031 D7 (b2): the local end (`http://127.0.0.1:<port>`) of each
    /// running `ssh -N -L` tunnel, keyed by `ssh`-kind connection id. Owned
    /// and kept current by the desktop's tunnel manager.
    ssh_local: Mutex<std::collections::HashMap<String, String>>,
}

impl ProxyState {
    pub fn new(secret: String, registry: Registry, assets: Box<dyn AssetSource>) -> Arc<Self> {
        Arc::new(Self {
            secret,
            registry: std::sync::Arc::new(Mutex::new(registry)),
            assets,
            http_client: reqwest::Client::new(),
            relay: Mutex::new(None),
            sessions: Mutex::new(std::collections::HashMap::new()),
            local_token_source: Mutex::new(Arc::new(crate::proxy::local_token::read)),
            local_password: Mutex::new(None),
            gate_client: crate::proxy::bounded::client(GATE_TIMEOUT),
            ssh_local: Mutex::new(std::collections::HashMap::new()),
        })
    }

    pub fn set_session(&self, connection_id: &str, token: String) {
        self.sessions.lock().unwrap().insert(connection_id.to_string(), token);
    }

    pub fn session(&self, connection_id: &str) -> Option<String> {
        self.sessions.lock().unwrap().get(connection_id).cloned()
    }

    pub fn clear_session(&self, connection_id: &str) {
        self.sessions.lock().unwrap().remove(connection_id);
    }

    fn current(&self) -> Connection {
        self.registry.lock().unwrap().current().clone()
    }

    /// set_local_token_source replaces how the `local` connection's daemon
    /// token is read (default: the token file, read per attempt).
    pub fn set_local_token_source(&self, source: impl Fn() -> Option<String> + Send + Sync + 'static) {
        *self.local_token_source.lock().unwrap() = Arc::new(source);
    }

    pub fn set_ssh_local(&self, connection_id: &str, local_url: String) {
        self.ssh_local.lock().unwrap().insert(connection_id.to_string(), local_url);
    }

    pub fn ssh_local(&self, connection_id: &str) -> Option<String> {
        self.ssh_local.lock().unwrap().get(connection_id).cloned()
    }

    pub fn clear_ssh_local(&self, connection_id: &str) {
        self.ssh_local.lock().unwrap().remove(connection_id);
    }

    /// upstream_base is the URL requests for `conn` are actually sent to:
    /// its own base URL, or for an `ssh` connection its tunnel's local end
    /// (`None` while no tunnel is running).
    pub fn upstream_base(&self, conn: &Connection) -> Option<String> {
        match conn.kind {
            ConnectionKind::Ssh => self.ssh_local(&conn.id),
            _ => Some(conn.base_url.clone()),
        }
    }

    /// current_upstream is `current()` with an `ssh` connection resolved to
    /// its tunnel (as a plain `url` connection), or the 502 text explaining
    /// that the tunnel is down.
    fn current_upstream(&self) -> Result<Connection, String> {
        let mut conn = self.current();
        if conn.kind == ConnectionKind::Ssh {
            let Some(local) = self.ssh_local(&conn.id) else {
                return Err(format!("smind desktop: the SSH tunnel for {} is not running; switch to it again to reconnect", conn.label));
            };
            conn.base_url = local;
            conn.kind = ConnectionKind::Url;
        }
        Ok(conn)
    }

    fn password_gate(&self, base_url: &str) -> Option<bool> {
        self.local_password.lock().unwrap().as_ref().filter(|(b, _)| b == base_url).map(|(_, v)| *v)
    }

    fn set_password_gate(&self, base_url: &str, protected: bool) {
        *self.local_password.lock().unwrap() = Some((base_url.to_string(), protected));
    }

    /// is_builtin_local: the connection whose credential may come from the
    /// token file -- the built-in `local` entry ONLY, never url/relay.
    fn is_builtin_local(conn: &Connection) -> bool {
        conn.id == LOCAL_ID && conn.kind == ConnectionKind::Local
    }

    async fn is_password_smind(&self, base_url: &str) -> bool {
        crate::proxy::local_token::is_password_smind(&self.gate_client, base_url).await
    }

    /// local_file_token reads the daemon token file now (it changes when the
    /// daemon restarts), only while `local` really is this machine
    /// (`SMIND_DAEMON_URL` can point it elsewhere).
    async fn local_file_token(&self, conn: &Connection) -> Option<String> {
        let base = Url::parse(&conn.base_url).ok()?;
        if !crate::proxy::local_token::is_local_by_address(&base).await {
            return None;
        }
        let source = self.local_token_source.lock().unwrap().clone();
        source()
    }

    /// upstream_bearer is the credential the proxy attaches upstream itself
    /// for `conn` (ADR-0031 D8), so the webview never holds it: the held
    /// login session for a connection that has one; else, for the built-in
    /// `local` connection only and only once its daemon is known to be
    /// password-protected (`password_gate`), the daemon token file.
    async fn upstream_bearer(&self, conn: &Connection) -> Option<String> {
        if let Some(token) = self.session(&conn.id) {
            return Some(token);
        }
        if !Self::is_builtin_local(conn) || self.password_gate(&conn.base_url) != Some(true) {
            return None;
        }
        self.local_file_token(conn).await
    }

    /// set_relay installs (or, with `None`, clears) the relay transport
    /// for the currently-selected connection -- called by whichever
    /// layer owns `connections_select` (the src-tauri command) whenever
    /// the selection changes.
    pub fn set_relay(&self, handle: Option<RelayHandle>) {
        *self.relay.lock().unwrap() = handle;
    }

    fn current_relay(&self) -> Option<RelayHandle> {
        self.relay.lock().unwrap().clone()
    }
}

/// build_router assembles the full proxy app: the secret gate wraps
/// every route below it, so a request with no valid cookie/`?k=` never
/// reaches the static handler, the HTTP proxy, or the WS bridge.
pub fn build_router(state: Arc<ProxyState>) -> Router {
    Router::new()
        .route("/ws", get(proxy_ws))
        .route("/api/{*rest}", any(proxy_http))
        .fallback(serve_asset)
        .layer(middleware::from_fn_with_state(state.clone(), gate))
        .with_state(state)
}

/// serve binds `127.0.0.1:0` (a random port), returning the bound port
/// and a future that runs the server forever -- the caller spawns that
/// future and uses the port to build the window's initial URL.
pub async fn serve(
    state: Arc<ProxyState>,
) -> std::io::Result<(u16, impl std::future::Future<Output = ()>)> {
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let port = listener.local_addr()?.port();
    let app = build_router(state);
    let fut = async move {
        if let Err(e) = axum::serve(listener, app).await {
            eprintln!("smind desktop: proxy server error: {e}");
        }
    };
    Ok((port, fut))
}

// --- the secret gate (AC3) ---------------------------------------------

async fn gate(State(state): State<Arc<ProxyState>>, req: Request<Body>, next: Next) -> Response {
    let cookie_header = req
        .headers()
        .get(header::COOKIE)
        .and_then(|v| v.to_str().ok());
    let query_k = query_param(req.uri().query(), "k");

    match secret::evaluate(&state.secret, cookie_header, query_k.as_deref()) {
        GateOutcome::Pass => next.run(req).await,
        GateOutcome::ExchangeSecret => {
            let redirect_to = strip_query_param(req.uri(), "k");
            let mut resp = Redirect::to(&redirect_to).into_response();
            let cookie = secret::build_set_cookie(&state.secret);
            resp.headers_mut().append(
                header::SET_COOKIE,
                HeaderValue::from_str(&cookie)
                    .expect("smind desktop: cookie header is valid ASCII"),
            );
            resp
        }
        GateOutcome::Forbidden => (StatusCode::FORBIDDEN, "forbidden").into_response(),
    }
}

fn query_param(query: Option<&str>, name: &str) -> Option<String> {
    let query = query?;
    url::form_urlencoded::parse(query.as_bytes())
        .find(|(k, _)| k == name)
        .map(|(_, v)| v.into_owned())
}

/// strip_query_param rebuilds `uri`'s path+query with `name` removed
/// from the query string -- used to drop `?k=...` before redirecting,
/// so the secret never lingers in the address bar or browser history.
fn strip_query_param(uri: &Uri, name: &str) -> String {
    let path = uri.path();
    let remaining: Vec<(String, String)> = uri
        .query()
        .map(|q| {
            url::form_urlencoded::parse(q.as_bytes())
                .filter(|(k, _)| k != name)
                .map(|(k, v)| (k.into_owned(), v.into_owned()))
                .collect()
        })
        .unwrap_or_default();
    if remaining.is_empty() {
        return path.to_string();
    }
    let qs = url::form_urlencoded::Serializer::new(String::new())
        .extend_pairs(&remaining)
        .finish();
    format!("{path}?{qs}")
}

// --- static assets, SPA fallback ----------------------------------------

async fn serve_asset(State(state): State<Arc<ProxyState>>, uri: Uri) -> Response {
    let path = uri.path().trim_start_matches('/');
    let path = if path.is_empty() { "index.html" } else { path };
    if let Some((bytes, content_type)) = state.assets.get(path) {
        return asset_response(bytes, &content_type);
    }
    // SPA fallback: any path that isn't a known asset (e.g. a
    // client-side hash route reached via reload) still gets index.html,
    // so the app's own router takes over from there.
    match state.assets.get("index.html") {
        Some((bytes, content_type)) => asset_response(bytes, &content_type),
        None => (
            StatusCode::NOT_FOUND,
            "smind desktop: no bundled UI assets found",
        )
            .into_response(),
    }
}

fn asset_response(bytes: Vec<u8>, content_type: &str) -> Response {
    Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, content_type)
        .body(Body::from(bytes))
        .expect("smind desktop: static asset response is well-formed")
}

// --- /api/* reverse proxy, streamed, Origin dropped ----------------------

/// HOP_BY_HOP_REQUEST_HEADERS are stripped from the inbound request
/// before it's forwarded upstream: `Host`/`Connection` describe *this*
/// hop, not the daemon's; `Origin` is dropped per ADR-0013 (the daemon's
/// `/ws` CheckOrigin only ever sees a same-origin or Origin-less
/// request, and dropping it here keeps `/api/*` and `/ws` consistent);
/// `Cookie` carries this proxy's own secret cookie, which the daemon
/// has no reason to ever see.
const HOP_BY_HOP_REQUEST_HEADERS: &[header::HeaderName] = &[
    header::HOST,
    header::CONNECTION,
    header::ORIGIN,
    header::COOKIE,
];

async fn proxy_http(State(state): State<Arc<ProxyState>>, req: Request<Body>) -> Response {
    let conn = match state.current_upstream() {
        Ok(c) => c,
        Err(msg) => return (StatusCode::BAD_GATEWAY, msg).into_response(),
    };
    let base = conn.base_url.clone();
    if conn.kind == ConnectionKind::Relay {
        return relay_api_response(req.uri());
    }
    let is_token_get = req.method() == axum::http::Method::GET && req.uri().path() == "/api/token";
    let has_session = state.session(&conn.id).is_some();
    if is_token_get && has_session {
        // The webview only needs *a* token to proceed; the real credential is
        // attached upstream and never leaves Rust.
        return placeholder_token_response();
    }
    // `local`'s own token fetch always asks the daemon (it refreshes what the
    // proxy knows about password protection), so it carries no credential.
    let bearer = if is_token_get { None } else { state.upstream_bearer(&conn).await };
    let session_in_use = bearer.is_some() && bearer == state.session(&conn.id);
    let path_and_query = req
        .uri()
        .path_and_query()
        .map(|pq| pq.as_str())
        .unwrap_or("/");
    let path_and_query = if bearer.is_some() {
        strip_query_param(req.uri(), "token")
    } else {
        path_and_query.to_string()
    };
    let url = format!("{}{}", base.trim_end_matches('/'), path_and_query);

    let method = req.method().clone();
    let mut headers = req.headers().clone();
    for name in HOP_BY_HOP_REQUEST_HEADERS {
        headers.remove(name);
    }
    if let Some(token) = &bearer {
        match bearer_header(token) {
            Some(v) => {
                headers.insert(header::AUTHORIZATION, v);
            }
            None => return (StatusCode::BAD_GATEWAY, "smind desktop: unusable credential").into_response(),
        }
    }
    let body_stream = req.into_body().into_data_stream();

    let upstream = state
        .http_client
        .request(method, &url)
        .headers(headers)
        .body(reqwest::Body::wrap_stream(body_stream))
        .send()
        .await;

    match upstream {
        Ok(resp) => {
            let status = resp.status();
            if session_in_use && matches!(status, StatusCode::UNAUTHORIZED | StatusCode::FORBIDDEN) {
                // The daemon no longer honours this session (expired): drop it,
                // so the next /api/token passes the daemon's own refusal
                // through and the webview shows its login screen.
                state.clear_session(&conn.id);
            }
            if is_token_get && ProxyState::is_builtin_local(&conn) {
                // 403 = a password-protected daemon (#349) that no longer
                // serves its token: authenticate with the token file when we
                // can read it. Anything else keeps the plain token flow.
                let protected = status == StatusCode::FORBIDDEN && state.is_password_smind(&conn.base_url).await;
                state.set_password_gate(&conn.base_url, protected);
                if protected && state.local_file_token(&conn).await.is_some() {
                    return placeholder_token_response();
                }
            }
            let mut headers = resp.headers().clone();
            headers.remove(header::CONNECTION);
            let body = Body::from_stream(resp.bytes_stream());
            let mut builder = Response::builder().status(status);
            *builder
                .headers_mut()
                .expect("smind desktop: response builder has headers") = headers;
            builder
                .body(body)
                .expect("smind desktop: proxied response is well-formed")
        }
        Err(e) => (
            StatusCode::BAD_GATEWAY,
            format!("smind desktop: proxy error: {e}"),
        )
            .into_response(),
    }
}

// --- relay `/api/token` synthesis (ADR-0013, ADR-0011) -------------------

/// relay_api_response answers `/api/*` for a `relay`-kind connection
/// locally, in Rust -- there is no daemon HTTP surface to reverse-proxy
/// to. Only `/api/token` is ever called by the bundled UI
/// (`web/packages/ui/src/lib/daemon.ts`'s `fetchToken`); the synthesized
/// value satisfies that `{token: string}` contract, but is not itself a
/// security boundary (see the plan's Decisions) -- the E2EE tunnel plus
/// the proxy's own per-launch secret cookie (checked by `gate`, above,
/// on every request including this one) are what actually gate access.
fn relay_api_response(uri: &Uri) -> Response {
    let rest = uri.path().strip_prefix("/api/").unwrap_or("");
    if rest == "token" {
        return placeholder_token_response();
    }
    (
        StatusCode::NOT_FOUND,
        "smind desktop: relay connections have no daemon HTTP surface",
    )
        .into_response()
}

/// placeholder_token_response is the synthesized `GET /api/token` answer: an
/// opaque random value, not a credential (see `relay_api_response`).
fn placeholder_token_response() -> Response {
    use base64::Engine;
    let mut buf = [0u8; 16];
    getrandom::fill(&mut buf).expect("getrandom: placeholder token");
    let token = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(buf);
    axum::Json(serde_json::json!({ "token": token })).into_response()
}

fn bearer_header(token: &str) -> Option<HeaderValue> {
    let mut v = HeaderValue::from_str(&format!("Bearer {token}")).ok()?;
    v.set_sensitive(true);
    Some(v)
}

/// The WebSocket subprotocol prefix a login session token is offered under
/// (`wsapi.BearerSubprotocolPrefix`): a browser cannot set headers on a
/// WebSocket, so the token rides there instead of in the URL.
const BEARER_SUBPROTOCOL_PREFIX: &str = "smind.bearer.";

/// offered_bearer_protocols lists the `smind.bearer.*` subprotocols the
/// browser offered, in order.
fn offered_bearer_protocols(headers: &HeaderMap) -> Vec<String> {
    headers
        .get_all(header::SEC_WEBSOCKET_PROTOCOL)
        .iter()
        .filter_map(|v| v.to_str().ok())
        .flat_map(|v| v.split(','))
        .map(str::trim)
        .filter(|p| p.starts_with(BEARER_SUBPROTOCOL_PREFIX))
        .map(str::to_string)
        .collect()
}

// --- /ws bridge, frame-by-frame, Origin-less upstream handshake ---------

async fn proxy_ws(
    State(state): State<Arc<ProxyState>>,
    RawQuery(query): RawQuery,
    headers: HeaderMap,
    ws: WebSocketUpgrade,
) -> Response {
    let conn = match state.current_upstream() {
        Ok(c) => c,
        Err(msg) => return (StatusCode::BAD_GATEWAY, msg).into_response(),
    };
    if conn.kind == ConnectionKind::Relay {
        return match state.current_relay() {
            Some(handle) => ws.on_upgrade(move |socket| bridge_relay(socket, handle)),
            None => (
                StatusCode::BAD_GATEWAY,
                "smind desktop: relay connection not active",
            )
                .into_response(),
        };
    }
    let mut target = match Url::parse(&conn.base_url) {
        Ok(u) => u,
        Err(_) => {
            return (
                StatusCode::BAD_GATEWAY,
                "smind desktop: invalid upstream URL",
            )
                .into_response()
        }
    };
    let scheme = if target.scheme() == "https" {
        "wss"
    } else {
        "ws"
    };
    // set_scheme rejects switching to/from a "special" scheme in some
    // `url` versions for certain hosts; ws/wss on an http(s) URL always
    // succeeds, so this is infallible in practice.
    let _ = target.set_scheme(scheme);
    target.set_path("/ws");

    // ADR-0031 D8: with a credential of its own (held session, or the local
    // daemon's token file) the proxy authenticates the upstream handshake
    // itself and drops whatever token the webview put in the URL; otherwise
    // the webview's own `smind.bearer.*` subprotocols are passed through.
    if ProxyState::is_builtin_local(&conn) && state.password_gate(&conn.base_url).is_none() {
        // The UI normally fetched /api/token first; if not, ask now.
        let probe = format!("{}/api/token", conn.base_url.trim_end_matches('/'));
        if let Ok(resp) = state.gate_client.get(probe).send().await {
            let protected = resp.status() == StatusCode::FORBIDDEN && state.is_password_smind(&conn.base_url).await;
            state.set_password_gate(&conn.base_url, protected);
        }
    }
    let bearer = state.upstream_bearer(&conn).await;
    let offered = offered_bearer_protocols(&headers);
    let query = match (&bearer, query.as_deref()) {
        (Some(_), Some(q)) => {
            let uri: Uri = format!("/ws?{q}").parse().unwrap_or_else(|_| Uri::from_static("/ws"));
            strip_query_param(&uri, "token").split_once('?').map(|(_, q)| q.to_string())
        }
        (_, q) => q.map(str::to_string),
    };
    target.set_query(query.as_deref());

    let mut request = match target.as_str().into_client_request() {
        Ok(r) => r,
        Err(_) => return (StatusCode::BAD_GATEWAY, "smind desktop: invalid upstream URL").into_response(),
    };
    match &bearer {
        Some(token) => match bearer_header(token) {
            Some(v) => {
                request.headers_mut().insert(header::AUTHORIZATION, v);
            }
            None => return (StatusCode::BAD_GATEWAY, "smind desktop: unusable credential").into_response(),
        },
        None if !offered.is_empty() => {
            if let Ok(v) = HeaderValue::from_str(&offered.join(", ")) {
                request.headers_mut().insert(header::SEC_WEBSOCKET_PROTOCOL, v);
            }
        }
        None => {}
    }

    // Dial upstream before accepting the browser: the browser aborts a socket
    // whose offered subprotocol the server did not select, so the answer has
    // to carry what the daemon chose.
    let (upstream, resp) = match connect_async(request).await {
        Ok(ok) => ok,
        Err(e) => {
            if let (tokio_tungstenite::tungstenite::Error::Http(resp), true) = (&e, bearer.is_some() && bearer == state.session(&conn.id)) {
                if matches!(resp.status().as_u16(), 401 | 403) {
                    state.clear_session(&conn.id);
                }
            }
            eprintln!("smind desktop: proxy ws upstream connect failed: {e}");
            return (StatusCode::BAD_GATEWAY, "smind desktop: upstream connection failed").into_response();
        }
    };
    let selected = match &bearer {
        // The upstream was not offered the browser's protocols: echo the
        // browser's own first choice so it does not abort.
        Some(_) => offered.first().cloned(),
        None => resp
            .headers()
            .get(header::SEC_WEBSOCKET_PROTOCOL)
            .and_then(|v| v.to_str().ok())
            .map(|v| v.trim().to_string())
            .filter(|v| offered.contains(v)),
    };
    let ws = match selected {
        Some(p) => ws.protocols([p]),
        None => ws,
    };
    ws.on_upgrade(move |socket| bridge(socket, upstream))
}

/// bridge pumps frames in both directions between the browser-side
/// `socket` and the already-open, Origin-less `upstream` connection
/// until either side closes -- the same no-Origin handshake the
/// existing `daemon-client::client` connection already relies on (see
/// ADR-0012), just used here for the browser's own connection instead
/// of the notification watcher's.
async fn bridge<S>(socket: WebSocket, upstream: tokio_tungstenite::WebSocketStream<S>)
where
    S: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin,
{
    let (mut up_tx, mut up_rx) = upstream.split();
    let (mut down_tx, mut down_rx) = socket.split();

    loop {
        tokio::select! {
            msg = down_rx.next() => {
                let Some(Ok(m)) = msg else { break };
                let closing = matches!(m, AxumMessage::Close(_));
                if up_tx.send(to_tungstenite(m)).await.is_err() || closing {
                    break;
                }
            }
            msg = up_rx.next() => {
                let Some(Ok(m)) = msg else { break };
                let closing = matches!(m, TMessage::Close(_));
                if let Some(am) = from_tungstenite(m) {
                    if down_tx.send(am).await.is_err() {
                        break;
                    }
                }
                if closing {
                    break;
                }
            }
        }
    }
}

/// bridge_relay pumps JSON-RPC messages between the browser-side
/// `socket` and `handle`'s shared relay tunnel: each inbound WS
/// text/binary message is sent as-is into the E2EE channel, and every
/// message the relay transport delivers is forwarded out as a WS text
/// frame -- the plaintext on both sides is the same `internal/wsapi`
/// JSON envelope the daemon's own `/ws` speaks, so the bundled UI needs
/// no relay-awareness at all. Unlike `bridge` (which dials a fresh
/// upstream WS per browser connection), this subscribes to the one
/// relay tunnel `crate::relay::client::spawn` already keeps alive
/// (reconnecting with backoff on its own) -- ending this browser
/// connection never tears the relay transport down.
async fn bridge_relay(socket: WebSocket, handle: RelayHandle) {
    let mut inbound = handle.subscribe();
    let (mut down_tx, mut down_rx) = socket.split();

    loop {
        tokio::select! {
            msg = down_rx.next() => {
                let Some(Ok(m)) = msg else { break };
                let bytes = match m {
                    AxumMessage::Text(t) => t.as_bytes().to_vec(),
                    AxumMessage::Binary(b) => b.to_vec(),
                    AxumMessage::Close(_) => break,
                    AxumMessage::Ping(_) | AxumMessage::Pong(_) => continue,
                };
                if handle.send(bytes).await.is_err() {
                    break;
                }
            }
            msg = inbound.recv() => {
                match msg {
                    Ok(bytes) => {
                        let text = String::from_utf8_lossy(&bytes).into_owned();
                        if down_tx.send(AxumMessage::Text(text.into())).await.is_err() {
                            break;
                        }
                    }
                    Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                    Err(tokio::sync::broadcast::error::RecvError::Closed) => break,
                }
            }
        }
    }
}

/// Close frames are forwarded as a bare close (no code/reason): the two
/// sides use independently-versioned `tungstenite` crates (axum's
/// internal one and this crate's own `tokio-tungstenite` dependency),
/// so their `CloseFrame`/`CloseCode` types aren't the same type -- and a
/// bare close still terminates the connection correctly, which is all
/// either peer observes.
fn to_tungstenite(msg: AxumMessage) -> TMessage {
    match msg {
        AxumMessage::Text(t) => TMessage::Text(t.as_str().into()),
        AxumMessage::Binary(b) => TMessage::Binary(b),
        AxumMessage::Ping(b) => TMessage::Ping(b),
        AxumMessage::Pong(b) => TMessage::Pong(b),
        AxumMessage::Close(_) => TMessage::Close(None),
    }
}

fn from_tungstenite(msg: TMessage) -> Option<AxumMessage> {
    match msg {
        TMessage::Text(t) => Some(AxumMessage::Text(t.as_str().into())),
        TMessage::Binary(b) => Some(AxumMessage::Binary(b)),
        TMessage::Ping(b) => Some(AxumMessage::Ping(b)),
        TMessage::Pong(b) => Some(AxumMessage::Pong(b)),
        TMessage::Close(_) => Some(AxumMessage::Close(None)),
        // Recommended by the tungstenite maintainers to ignore raw
        // `Frame` frames (https://github.com/snapview/tungstenite-rs/issues/268);
        // axum's own from_tungstenite does the same.
        TMessage::Frame(_) => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn query_param_finds_k() {
        assert_eq!(query_param(Some("k=abc&x=y"), "k"), Some("abc".to_string()));
        assert_eq!(query_param(Some("x=y"), "k"), None);
        assert_eq!(query_param(None, "k"), None);
    }

    #[test]
    fn strip_query_param_removes_only_named_param() {
        let uri: Uri = "/?k=abc".parse().unwrap();
        assert_eq!(strip_query_param(&uri, "k"), "/");

        let uri: Uri = "/foo?k=abc&x=y".parse().unwrap();
        assert_eq!(strip_query_param(&uri, "k"), "/foo?x=y");

        let uri: Uri = "/foo".parse().unwrap();
        assert_eq!(strip_query_param(&uri, "k"), "/foo");
    }

    #[test]
    fn relay_api_response_synthesizes_token() {
        let uri: Uri = "/api/token".parse().unwrap();
        let resp = relay_api_response(&uri);
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[test]
    fn relay_api_response_rejects_everything_else() {
        let uri: Uri = "/api/healthz".parse().unwrap();
        let resp = relay_api_response(&uri);
        assert_eq!(resp.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn bridge_relay_pumps_both_directions() {
        let (handle, mut outbound_rx, inbound_tx, _status_tx) = crate::relay::client::test_handle();

        // Build a real WS pair (client <-> proxy) via an in-process
        // server so `bridge_relay` runs against a genuine `WebSocket`.
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let app = Router::new().route(
            "/ws",
            axum::routing::get(move |ws: WebSocketUpgrade| async move {
                ws.on_upgrade(move |s| bridge_relay(s, handle))
            }),
        );
        tokio::spawn(async move {
            let _ = axum::serve(listener, app).await;
        });

        let (mut ws, _) = tokio_tungstenite::connect_async(format!("ws://{addr}/ws"))
            .await
            .unwrap();

        // Browser -> relay: a client message must reach the relay handle.
        ws.send(TMessage::text("outbound from browser"))
            .await
            .unwrap();
        let forwarded = outbound_rx.recv().await.unwrap();
        assert_eq!(forwarded, b"outbound from browser");

        // Relay -> browser: a message pushed onto the relay's inbound
        // channel must reach the WS client.
        inbound_tx.send(b"inbound from relay".to_vec()).unwrap();
        let received = ws.next().await.unwrap().unwrap();
        assert_eq!(received.into_text().unwrap(), "inbound from relay");

        ws.close(None).await.ok();
    }
}
