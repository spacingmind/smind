# Relay (Phase 3) — trạng thái thiết kế

Audit read-only tính đến 2026-09-11. Mục tiêu: xác định "relay" (relay server E2EE
cho daemon↔mobile, roadmap Phase 3) đã được quyết định/thiết kế tới đâu, trước khi
bắt đầu implement.

## 1. Tài liệu trong docs/ — mention vs thiết kế thật

Toàn bộ chuỗi "relay" trong docs/ nằm ở đúng 6 file. Không file decision/plan/research
nào khác nhắc tới relay.

| File | Vai trò | Ghi chú |
|---|---|---|
| `docs/ROADMAP.md` (Phase 3, dòng 51-69) | **THIẾT KẾ THẬT** | Checklist đầy đủ, nhưng toàn bộ đều `- [ ]` (chưa làm) |
| `docs/ROADMAP.md` (Phase 2, dòng 40) | Mention | Chỉ nhắc gRPC "planned for daemon↔relay" để biện minh cho lựa chọn WS-RPC của Phase 2 |
| `docs/decisions/0003-agpl-license-no-repo-split.md` | **THIẾT KẾ THẬT (nhưng tự nhận là chưa chốt)** | Dự đoán relay sẽ tách repo riêng (`smind-relay`), license permissive — ADR tự ghi rõ "not decided as a live thing today" |
| `docs/plans/active/oauth-account-login.md` | Mention | Ghi nhận giới hạn: flow OAuth giả định daemon+browser chung `localhost`, sẽ vỡ khi có relay — "known limitation, not solved here" |
| `docs/plans/completed/daemon-restart-resync.md` | Mention | Relay chỉ được nêu để biện minh lý do build reconnect logic vào web client dùng chung (sau này mobile qua relay tái dùng) |
| `docs/research/dual-mode-ui.md` | Mention (trong 1 open question) | Cảnh báo: bất kỳ quyết định API subscription/push nào hôm nay cũng phải "sống sót" qua relay ở Phase 3 → khóa sớm quyết định daemon API |
| `docs/research/codmon-chat-server-notes.md` | Mixed, chủ yếu mention | So sánh với codmon, nhiều lần nói "Reject for now, revisit at Phase 3"; xác nhận lại "reconnect grace + key rotation" là "the one open question" |

**Nội dung ROADMAP.md Phase 3 (verbatim, để tham chiếu):**
- Relay server (Go, dumb pipe), self-hostable as `smind relay`
- E2EE handshake: X25519 + ChaCha20-Poly1305, QR pairing
- Reconnect grace, correct key rotation
- Mobile app (Expo + @expo/ui): pairing + workspace/task list, realtime agent
  timeline + follow-up, push notifications, mobile permission approval
- Deploy relay tại `relay.spacingmind.sh` (Cloudflare TLS)

Transport note (Phase 2, dòng 40 & note trong Phase 3): daemon↔relay dùng gRPC vì
đó là "a real service boundary" (relay deploy riêng), khác API browser-facing của
Phase 2 (WS-RPC, vì gRPC không có browser support native). Roadmap tự ghi rõ:
"Not implemented yet; noted here so Phase 3 design starts from this rather than
re-litigating it" — tức đây là một pre-decision có chủ đích, không phải đã build.

## 2. Code đã viết cho relay — không có gì

Quét `internal/`, `cmd/`, `web/`, `go.mod/go.sum`:

- **Không có** package/struct/interface nào tên `Relay*`, không `cmd/relay`, không
  subcommand `smind relay` đăng ký trong `cmd/smind/main.go`.
- **Không có** file `.proto` nào của smind (chỉ có `.proto` vendored trong
  `refs/codex/...`, không liên quan, không build).
- **Không có** dependency QR code, X25519, ChaCha20-Poly1305 trực tiếp trong
  `go.mod`. ChaCha20-Poly1305 chỉ xuất hiện gián tiếp qua `golang.org/x/crypto`
  vì `internal/accounts` dùng `github.com/refraction-networking/utls` để giả
  TLS ClientHello khi gọi OAuth — đây là chi tiết TLS cipher-suite của thư viện
  bên thứ ba, **không phải** groundwork cho E2EE handshake của relay.
- 4 chỗ khớp chuỗi "relay" trong Go code (`internal/runs/subqueue.go`,
  `internal/terminal/subqueue.go` và 2 `registry.go` tương ứng) là hàm nội bộ
  `relay(q, out, done)` — một helper fan-out event queue, trùng tên tiếng Anh
  ngẫu nhiên, không liên quan tính năng relay.
- `web/` (frontend) không có bất kỳ tham chiếu "relay" nào, không có scaffold
  mobile/Expo app.
- Không có Dockerfile/docker-compose/systemd unit nào cho relay (hay cho chính
  daemon) ở cấp repo.

**Kết luận mục 2:** 0% code, 0% scaffold, 0% config placeholder cho relay.

## 3. Quyết định còn ngỏ (chưa chốt chi tiết)

- **Giao thức gRPC**: mới ở mức "planned"/nguyên tắc, chưa có `.proto`, chưa có
  service definition, chưa validate được việc gRPC-Go daemon nói chuyện với relay
  ra sao (streaming bidirectional? multiplexing nhiều mobile client trên 1 kết nối
  daemon?). Roadmap chỉ nói "gRPC ruled out for Phase 2, planned for Phase 3" —
  chưa có thiết kế message/service.
- **Cơ chế pairing/QR chi tiết**: roadmap chỉ ghi "QR pairing", không có đặc tả
  wire format, không nói QR chứa gì (public key? connection offer? relay
  endpoint?), không nói ai render QR (daemon hay mobile), không có UX flow.
- **Mô hình relay state ("dumb pipe" nghĩa là gì chính xác)**: chưa được định
  nghĩa rõ trong docs. Câu hỏi mở, chưa trả lời:
  - Relay có lưu message khi mobile offline không (store-and-forward), hay chỉ
    forward khi cả hai đầu đang mở kết nối?
  - Relay có fanout cho nhiều thiết bị mobile trên cùng 1 daemon không (roadmap
    chỉ nói "mobile app", số ít, không rõ multi-device)?
  - `docs/research/codmon-chat-server-notes.md` gợi ý vấn đề multi-device push
    fan-out "chỉ xuất hiện khi relay push tới multiple mobile devices" và tự
    quyết định "Reject for now, revisit at Phase 3" — tức đây là câu hỏi được
    biết trước nhưng cố tình hoãn.
- **Key rotation chi tiết**: roadmap chỉ liệt kê "correct key rotation" như một
  yêu cầu, không có cơ chế. `codmon-chat-server-notes.md` gọi thẳng đây là "the
  one open question" chưa ai giải.
- **Auth giữa daemon và relay**: chưa có đặc tả. `codmon-chat-server-notes.md`
  chỉ khẳng định nguyên tắc ("không được là unauthenticated internal RPC vì
  network không trusted"), dẫn chiếu ngược lại ROADMAP Phase 3 E2EE handshake,
  nhưng bản thân ROADMAP không có cơ chế auth daemon↔relay tách biệt khỏi E2EE
  handshake giữa daemon và mobile (2 lớp này có gộp làm một không? chưa rõ).
- **Deploy topology**: chốt duy nhất 1 điểm — hostname `relay.spacingmind.sh`
  + Cloudflare TLS. Chưa có: single-tenant vs multi-tenant relay, có bao nhiêu
  daemon per relay instance, self-host instructions, scaling/persistence layer
  (in-memory dumb pipe hay cần state store?).
- **Repo split**: ADR-0003 dự đoán relay sẽ là repo riêng (`smind-relay`),
  license permissive — nhưng tự ghi "not decided as a live thing today".

## 4. Tham khảo refs/

- **refs/paseo — precedent trực tiếp, gần sát với ý tưởng smind's roadmap.**
  Paseo đã build, test, document đầy đủ một relay dumb-pipe tương tự:
  - `packages/relay/src/crypto.ts`: X25519 (Curve25519 ECDH) nhưng dùng
    **XSalsa20-Poly1305** (NaCl `box`), KHÁC với ChaCha20-Poly1305 mà smind
    roadmap chốt — cần quyết định rõ ràng thay vì copy nguyên Paseo.
  - `packages/relay/src/encrypted-channel.ts`: handshake state machine
    (`e2ee_hello`/`e2ee_ready`), re-hello với key khác trên channel đang mở
    bị coi là tấn công và đóng socket (code 1008) — tức "key rotation" của
    Paseo thực chất là "phiên mới = key mới", KHÔNG có live rekey.
  - `packages/relay/src/cloudflare-adapter.ts`: dumb pipe thật (Cloudflare
    Durable Object, forward byte thô, không giải mã); có protocol v1 (1
    server-client pair) vs v2 (control socket + nhiều data socket per client)
    — tức đã trả lời câu hỏi "fanout nhiều device" bằng thiết kế v2; có
    store-and-forward buffer tối đa 200 frame khi phía kia chưa kết nối lại
    (đây chính là "reconnect grace" ở tầng relay).
  - `packages/server/src/server/relay-transport.ts` + `daemon-keypair.ts` +
    `connection-offer.ts` + `pairing-offer.ts`: daemon giữ X25519 keypair bền
    (`$PASEO_HOME/daemon-keypair.json`, mode 0600), pairing offer đóng gói
    serverId + public key + relay endpoint, encode vào URL fragment (không lên
    server) rồi render QR; mobile sinh keypair ephemeral mỗi phiên.
  - `SECURITY.md` có sẵn threat model relay bằng văn bản, và tự thừa nhận một
    lỗ hổng: **không có replay protection trong phiên sống** (no nonce/counter
    tracking) — smind nên quyết định rõ có chấp nhận rủi ro này hay không thay
    vì kế thừa ngầm.
  - Transport là WebSocket thuần, không phải gRPC — nếu smind giữ quyết định
    gRPC, các pattern quản lý session/reconnect/backoff của Paseo vẫn áp dụng
    được về mặt khái niệm, nhưng wire framing sẽ phải thiết kế lại.

- **refs/cliproxyapi — không phải precedent cho relay, chỉ hữu ích như phản ví dụ.**
  `internal/wsrelay/` là một RPC-over-WebSocket có trạng thái (chuyển đổi
  HTTP request/response qua envelope, dùng cho 1 provider cụ thể — AI Studio
  qua browser extension), ngược hoàn toàn với triết lý "dumb pipe". Không có
  gRPC, không E2EE, không QR/pairing — auth chỉ là API key phẳng
  (`Authorization: Bearer <key>`), `CheckOrigin` luôn `true`. Giá trị duy nhất
  cho smind: minh họa mô hình auth KHÔNG nên copy (flat shared secret, không
  per-device key, không rotation).

## 5. Kết luận: đã chốt đến đâu

**Mức độ tổng thể: "đã chốt giao thức" ở phần khung sườn, nhưng phần lớn chi
tiết vẫn ở mức "chỉ-ý-tưởng". Chưa có thiết kế chi tiết, chưa có code.**

Cụ thể theo từng mảnh:
- Kiến trúc tổng quát (dumb pipe, Go, self-hostable `smind relay`): **đã chốt
  giao thức** (quyết định nguyên tắc, ghi trong ROADMAP, chưa implement).
- Transport daemon↔relay = gRPC: **đã chốt giao thức** ở mức lựa chọn công
  nghệ, nhưng chưa có service/message definition → chưa phải "thiết kế chi
  tiết".
- Crypto primitives (X25519 + ChaCha20-Poly1305): **đã chốt giao thức** (tên
  thuật toán cụ thể), nhưng chưa có đặc tả handshake flow, framing, hay quyết
  định về replay protection.
- QR pairing: **chỉ-ý-tưởng** (chỉ là tên gọi, chưa có cơ chế).
- Reconnect grace + key rotation: **chỉ-ý-tưởng**, tự nhận là "the one open
  question" trong chính docs của repo.
- Relay state model (offline store-and-forward, multi-device fanout): **chỉ-ý-tưởng**,
  chưa có câu trả lời, cố tình hoãn tới Phase 3.
- Auth daemon↔relay riêng biệt: **chỉ-ý-tưởng**, chưa phân biệt rõ với E2EE
  handshake daemon↔mobile.
- Deploy topology: **chỉ-ý-tưởng** ngoại trừ 1 chi tiết đã chốt (hostname +
  Cloudflare TLS).
- Repo split / license: **đã có quyết định dự kiến nhưng tự nhận chưa chốt**
  (ADR-0003, "not decided as a live thing today").
- Code: **chưa có gì** — 0 scaffold, 0 proto, 0 dependency, 0 subcommand.

Khoảng cách lớn nhất trước khi có thể bắt đầu implement: (1) viết một ADR/plan
riêng cho relay quyết định rõ state model (store-and-forward? multi-device
fanout?) và cơ chế key rotation thật sự; (2) chốt xem có kế thừa pattern của
refs/paseo (đặc biệt là cấu trúc control-socket + data-socket, và cách daemon
giữ keypair bền) hay tự thiết kế lại cho gRPC; (3) quyết định rõ ràng
ChaCha20-Poly1305 vs XSalsa20-Poly1305 và lý do khác với Paseo.

---

## 6. Cập nhật 2026-09-17 — research qua `pplx` (Perplexity Pro), dogfood qua smind

Từ audit ở trên (2026-09-11), **ADR-0007** đã chốt kiến trúc relay (blind
forwarder, X25519 + ChaCha20-Poly1305 counter-nonce 12-byte, QR pairing qua
URL-fragment offer, bounded store-and-forward buffer — tiền lệ paseo 200
frame/side). Phần "reconnect grace + key rotation" từng là "the one open
question" giờ có dữ kiện cụ thể từ 3 câu hỏi research bên ngoài, chạy qua
`pplx ask -m best` (Perplexity Pro, dogfood qua smind task runner):

**Durable Objects vs self-hosted Go relay**: DO chỉ thắng khi có rất nhiều
kết nối gần-như-idle (nhờ WebSocket Hibernation billing 20:1); với một
region/daemon↔relay đơn lẻ như ADR-0007 mô tả, Go tự host rẻ hơn bằng tiền
mặt VÀ đơn giản hơn về cold-start (DO Hibernation xoá state trong RAM, buộc
buffer phải sống trong DO SQLite storage để đúng — Go giữ buffer RAM, không
cold-start, đổi lại phải tự lo persist qua crash/deploy). Xác nhận lựa chọn
"Go, self-hostable" của ADR-0007 là đúng hướng, không cần Cloudflare.

**Push notifications**: nên dùng kiểu "wake-up-only" như Signal/Delta Chat
(ping rỗng/token đã mã hoá, không nội dung/kích thước/sender) thay vì
encrypted-content-push kiểu Session — khớp triết lý "dumb pipe" của relay.
Chấp nhận rò rỉ: push provider (FCM/APNs) biết device token + thời điểm.
Web Push/VAPID không thay thế được FCM/APNs cho mobile timely delivery.

**X25519 + ChaCha20-Poly1305 rekey/nonce**: quyết định (e) "session mới =
key mới, không rekey khi đang sống" của ADR-0007 an toàn với 4 điều kiện:
(1) reconnect luôn tạo ephemeral handshake mới, không bao giờ resume counter
dưới key cũ; (2) counter theo hướng (d) phải monotonic và persist-hoặc-bỏ —
mất state thì bỏ key, không đoán; (3) nhãn hướng (c2s/s2c) phải bind vào KDF
key riêng; (4) soft limit về số message/key thấp hơn nhiều 2^64 (WireGuard
dùng ngưỡng 2^60 message HOẶC 120s — smind sẽ chạm mốc thời gian trước).

Findings đầy đủ (3 query, kèm URL nguồn): xem lịch sử dogfood run
`f157f84c04d59b16faa75718a15906d4` hoặc bản lưu cục bộ lúc chạy tại
`/tmp/relay-research/findings.md` (không nằm trong repo).

**Phát hiện phụ, về chính smind (không phải relay)**: `task send --approval-policy
auto-safe` không cover việc gọi một binary ngoài (như `pplx`) hay các lệnh
đọc đơn giản như `ls`/`mkdir` — safeCommandPrefixes chỉ allowlist
gofmt/go vet/go test/task test|lint|build/git add|commit
(`internal/taskrunner/policy.go`). Một dogfood run headless (không ai theo
dõi permission card) sẽ bị timeout-deny sau 5 phút cho mỗi lệnh ngoài
allowlist rồi tự thử lại — có thể tốn rất nhiều thời gian chờ nếu không có
người chủ động trả lời qua `run.respondPermission`. Không phải bug (đúng
thiết kế an toàn), nhưng là giới hạn thật của "dogfood tự động hoá task
research" cần biết trước khi giao việc tương tự cho agent không giám sát.

---

## 7. Cập nhật 2026-09-17 (tiếp) — 3 câu hỏi còn lại: pairing, fanout, auth daemon↔relay

Đào tiếp 3 gap "chỉ-ý-tưởng" còn lại trong mục 5 (QR pairing cơ chế, relay state
model multi-device/overflow, auth daemon↔relay tách biệt E2EE) bằng 3 query
`pplx ask -m best` còn lại (6/6 quota tuần đã dùng hết cho research relay).

**QR pairing — cơ chế cụ thể (Signal/WhatsApp/Session đều cùng một khuôn
mẫu):** QR chỉ chứa một **ephemeral public key + rendezvous id** (không bao
giờ chứa long-term secret); thiết bị đã tin cậy scan QR rồi mã hoá bundle
(identity key, credential, linking token) bằng khoá ephemeral đó trước khi
gửi qua relay — relay chỉ thấy ciphertext + UUID phiên, không bao giờ thấy
private key hay nội dung giải mã. Signal dùng đúng kỹ thuật ADR-0007 đã chọn:
key nằm trong **URL fragment** (`#...`) vì fragment không bao giờ được gửi
lên server theo RFC 3986 §3.5 — server chỉ thấy `https://.../` trần, không
thấy phần sau `#`. WhatsApp thay vào đó dùng payload opaque (không phải URL)
nên không cần kỹ thuật fragment. Rủi ro còn lại không giải quyết được bằng
crypto: **QR-substitution** (kẻ tấn công tráo QR hiển thị) và **screenshot/
chia sẻ màn hình** — hai lớp phòng thủ bổ sung là (a) hết hạn QR rất nhanh
(20-60s, theo WhatsApp) và (b) hiện device-fingerprint/safety-number để
người dùng verify thủ công sau khi pair.

**Relay state model — multi-device fanout + overflow:** kết luận đồng nhất
qua cả Signal, Matrix, Session: **mỗi device một queue riêng, không gộp theo
user** — vì relay không giải mã được nên không thể demux theo user sau khi
mã hoá; sender phải mã hoá riêng cho từng device (client-side fanout).
Ack/delivery là **at-least-once ở tầng transport dựa trên message-id opaque**
(không phải nội dung) — "delivered"/"read" receipt là dữ liệu E2EE riêng gửi
ngược lại, relay chỉ biết "ciphertext X đã được 1 device ack", không biết
user có đọc hay chưa. Ordering dựa vào **id/timestamp đơn điệu do relay gán**
(nằm ngoài phần mã hoá) để client tự sort khi merge sau reconnect — khớp
đúng ADR-0007 (đếm hướng qua counter, không suy luận thứ tự từ nội dung).
Overflow: mọi hệ thống tham chiếu đều **drop-oldest trong buffer bounded**
(Signal ~30-46 ngày TTL, Matrix theo config retention) rồi bắt client
**force full resync** khi phát hiện gap — không hệ thống nào cố "vá" gap
bằng cách relay tự suy luận lại. Đây xác nhận chính xác lựa chọn "bounded
buffer, drop-oldest" của ADR-0007 (tiền lệ paseo 200 frame/side) là đúng
kiểu thiết kế chuẩn ngành, không phải giản lược tạm.

**Auth daemon↔relay (tách biệt khỏi E2EE handshake daemon↔mobile):**
khuyến nghị rõ ràng — **KHÔNG dùng thành công E2EE handshake làm bằng chứng
daemon được phép nói chuyện với relay**; đây phải là 2 state machine độc
lập. Thiết kế đề xuất, phù hợp nhất cho 1 Go binary self-host, không cần CA
ngoài:
- **TLS transport**: relay tự ký cert, daemon **pin fingerprint lúc pairing**
  (không cần CA ngoài, giống cách chisel pin server key fingerprint).
- **Admission theo workspace**: mỗi workspace có secret ngẫu nhiên
  256-bit, relay chỉ lưu **hash** của secret (không lưu plaintext); daemon
  xác thực qua **challenge-response HMAC** (nonce + transcript), không gửi
  lại token trần mỗi lần — chống replay.
- **Nâng cấp tuỳ chọn**: daemon tạo cặp khoá Ed25519, đăng ký public key khi
  pairing; sau đó ký transcript thay vì gửi lại secret — bearer token chỉ
  còn dùng để bootstrap/re-pair, không dùng cho mọi kết nối thường.
- **So sánh 3 lựa chọn**: bearer token (đơn giản nhất, đủ dùng nếu có
  challenge-response + hash-at-rest + TLS pinning), mTLS pinned tại pairing
  (định danh thiết bị mạnh hơn, không cần CA ngoài — vẫn khả thi bằng
  `crypto/tls`+`crypto/x509` chuẩn của Go), macaroons/biscuits (chỉ đáng
  dùng nếu cần delegation/scope/expiry thật — thừa cho 1 relay tự host đơn
  giản, relay đã là authority duy nhất).
- **Tiền lệ công cụ tương tự**: Tailscale DERP (relay mù nhưng vẫn verify
  client thuộc domain định danh), ntfy (bearer token per-identity + ACL,
  revoke/expire được), rathole (shared token per-service), chisel (auth
  file + server-fingerprint pin) — tất cả đều tách rõ "ai được nói chuyện
  với relay" khỏi "nội dung được mã hoá ra sao".

**Kết luận cho ADR-0007 / plan tiếp theo**: cả 3 mảng "chỉ-ý-tưởng" còn lại
trong mục 5 giờ có thiết kế cụ thể, khớp hướng đã chọn (blind forwarder,
bounded buffer, per-direction counter). Việc còn thiếu trước khi code: viết
rõ **admission handshake** (workspace secret + HMAC challenge-response +
cert pinning) thành một phần của ADR-0007 hoặc một ADR con riêng, vì đây là
quyết định kiến trúc (auth layer tách biệt E2EE) chưa được ghi ở đâu trước
đó.

