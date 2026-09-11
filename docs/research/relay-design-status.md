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
