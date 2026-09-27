# Đánh giá lại mô hình release của smind

Ngày: 2026-09-27. Báo cáo này đọc hiện trạng release của smind, đối chiếu với
báo cáo khảo sát mà CatchM (spacingmind/catchm) đã dùng để quyết định từ bỏ
release-please + master/develop, rồi khuyến nghị mô hình cho chính smind.
Chỉ đọc repo + GitHub API; không đổi code/workflow.

## 1. Hiện trạng release của smind (bằng chứng)

### 1.1 Các thành phần

| Thành phần | File / vị trí | Vai trò |
|---|---|---|
| release-please | `.github/workflows/release-please.yml` | Trên push `master`: đọc Conventional Commits, mở/merge release PR → tạo tag + GitHub Release → build binaries (linux/darwin × amd64/arm64, tar.gz) + Windows installers (gọi `desktop-windows.yml`) → `checksums.txt` → attach vào Release |
| sync-back | `.github/workflows/sync-develop.yml` | Trên push `master`: nếu develop chưa chứa commit của master, mở PR merge thật (`merge`, không squash/rebase) master → develop |
| CI guard | `ci.yml` step "Guard release metadata" + `scripts/check-release-metadata.sh` | Với PR base `master`_only_: fail nếu manifest version lùi hoặc CHANGELOG mất entry đã có trên master |
| Config | `release-please-config.json` | 1 package tại root; bump version thêm ở `desktop/package.json`, `desktop/src-tauri/tauri.conf.json`, 2 `Cargo.toml` (`extra-files`) |
| Manifest | `.release-please-manifest.json` | `{"." : "0.7.0"}` |
| Rulesets | GitHub API (repo public — miễn phí) | `master`: required linear history, chỉ cho **rebase** merge, required check `ci`, cấm force-push/delete. `develop`: cho merge/squash/rebase, required `ci`, cấm force-push/delete |
| Docs | `CONTRIBUTING.md` | Mô tả branching model + cảnh báo incident |

### 1.2 Lịch sử release thực tế

- Tags: `v0.2.0 … v0.7.0` (6 releases, cuối là v0.7.0 ngày 2026-09-15).
- **Tất cả 6 releases đều 0 assets** (`gh api releases` → `assets: []` cho
  mọi tag) dù README mục "Installing the daemon from a release" hướng dẫn
  tải `smind_<v>_<os>_<arch>.tar.gz`. Job build+attach chỉ được thêm bởi
  PR #198, merged **2026-09-25** — sau mọi release tính đến nay. Ba run
  `workflow_dispatch` ngày 25/9 (dry-run) đều success, tức pipeline binary
  đã được chứng minh chạy được nhưng **chưa từng gắn vào release thật**.
  Hiện người dùng thực tế lấy binary qua `go install …@latest` hoặc tự build.
- Incident thật (đã được báo cáo CatchM nhắc đúng): v0.6.0 → v0.7.0.
  PR #135 (promote develop→master bằng rebase, sync tree bằng develop)
  clobber `.release-please-manifest.json` + `CHANGELOG.md` về trạng thái
  v0.5.0 vì develop chưa hấp thụ release commit; release-please proposing
  re-release toàn bộ trong #136 (closed); #137 vá thủ công; #138 release
  v0.7.0.

### 1.3 Phát hiện mới: pipeline release đang stall từ 2026-09-17

Đây là dữ liệu báo cáo CatchM **không có** và nặng hơn incident metadata:

1. Sau v0.7.0, sync-develop (#139) và các PR-branch updates đưa **merge
   commits thật** vào lịch sử `develop` (đúng như thiết kế của chính
   sync-develop — nó cố tình dùng merge thật để ancestor-check đúng).
2. Ruleset `master` chỉ cho phép **rebase-merge** + linear history. GitHub
   không thể replay merge commits → PR #154 (promote) không merge được,
   bị đóng.
3. PR #155 phải thay thế: tạo branch 1 commit duy nhất có **tree byte-identical
   với develop tip** rồi rebase-merge — squash 125 commits thành một commit
   `chore(master): release develop into master (125 commits since v0.7.0)`
   (mergeCommit `1c45c08`, đúng 1 parent `8a29705`).
4. Hậu quả: release-please trên master chỉ thấy 1 commit `chore:` — log run
   35261251101 ghi rõ: `No user facing commits found since 8a29705…
   skipping`. Không có release PR nào được mở từ đó đến nay.
5. Hiện tại: `develop` ahead of v0.7.0 **187 commits**, `master` chỉ có 1
   commit promotion trơ trơ, 10 ngày không release được gì dù có thay đổi
   lớn (multi-chat ADR-0016, desktop managed daemon, ZCode parity…).

Điểm mấu chốt: **3 lớp bảo vệ (sync-develop, check-release-metadata,
ruleset) không phát hiện stall này**. Chúng chỉ bảo vệ *metadata regression*;
lỗ hổng mới nằm ở chỗ promotion squash làm mất toàn bộ Conventional Commit
messages — thứ release-please cần để tồn tại. Mô hình hiện tại tự mâu thuẫn:
sync-develop tạo merge commits vào develop (để ancestor-check đúng) → chính
những merge commits đó làm promotion kế tiếp không thể rebase → buộc squash
→ release-please mù. Đây là vòng lặp tự phá hoại có cấu trúc, không phải lỗi
vận hành một lần.

## 2. Đối chiếu với báo cáo CatchM

| Nhận định của CatchM | Đúng/sai với smind | Ghi chú |
|---|---|---|
| "3 lớp bảo vệ chỉ để vá lỗ hổng sync-back; single-branch không có cơ hội phát sinh lỗ hổng đó" | **Đúng, và còn thấp hơn thực tế** | Lớp vá chỉ phủ 1 trong 2 lần vỡ. Lần vỡ thứ 2 (stall sau #155) là do chính cấu trúc master/develop + rebase-only ruleset, không do quên sync |
| "Đã gây incident thật (v0.6.0→v0.7.0, #135/#136/#137)" | Đúng | Xác nhận qua PR bodies và git history |
| "rulesets trả phí (private repo)" | **Không áp dụng cho smind** | smind là repo public → rulesets miễn phí và đang dùng thật (2 ruleset active, xác nhận qua API). Đây là chi phí CatchM chịu, không phải smind |
| "smind là mô hình yếu nhất trong khảo sát" | Đúng theo bằng chứng | Cả 3 dự án khảo sát, smind là dự án duy nhất vỡ 2 lần, lần thứ 2 khiến pipeline đứng hẳn |
| (CatchM không đề cập) smind không có binary publish | **Đã lỗi thời** | Tại thời điểm khảo sát đúng; nay đã có workflow build+attach (PR #198, dry-run green) nhưng chưa từng chạy cho release thật vì… pipeline đang stall (mục 1.3). Hai vấn đề cộng hưởng |
| "CHANGELOG tự động không đáng giá so với rủi ro" (với CatchM 1-maintainer) | **Khác với smind** | smind public, có người dùng thật (README hướng dẫn install từ Release, `go install @latest`), và release-please còn bump version đồng bộ 4 file desktop (`package.json`, `tauri.conf.json`, 2 `Cargo.toml`) — giá trị này CatchM không cần, smind có |

Kết luận đối chiếu: báo cáo CatchM đúng hướng và thậm chí còn *được xác thực
thêm* bởi dữ liệu smind (stall đang diễn ra). Nhưng lý do cụ thể và trao đổi
giá trị (trade-off) của CatchM không chuyển nguyên sang smind được: CatchM bỏ
release-please vì 1-maintainer + không cần changelog; smind thì ngược lại —
changelog tự động + version sync nhiều file + binary attach là giá trị thật.

## 3. Khuyến nghị

**Đề xuất: phương án (c) — giữ release-please, bỏ nhánh `master`. Single
integration branch (`develop`), release-please chạy trực tiếp trên nhánh đó,
`master` thành con trỏ fast-forward trỏ đúng tag đã release (như vai trò
master trong mô hình CatchM).**

Lý do, theo bằng chứng:

1. **Mô hình hiện tại đã chết trên thực tế, không phải "rủi ro lý thuyết".**
   Release gần nhất cách 12 ngày, 187 commits kẹt, và con đường promote hợp
   lệ duy nhất theo ruleset (rebase-merge) đã được chứng minh là bất khả
   thi mỗi khi develop chứa merge commit — điều guaranteed bởi chính
   sync-develop workflow. Giữ nguyên (a) đồng nghĩa với chấp nhận squash
   promotion → release-please mù vĩnh viễn, hoặc cấm merge commits trên
   develop (phá sync-develop và workflow PR-update của người đóng góp).
2. **Nhánh `master` không mang lại giá trị nào cho smind.** Nó không phải
   merge target thường xuyên (CONTRIBUTING cấm), nội dung tree của nó luôn
   bằng develop tip tại thời điểm promote, và toàn bộ cơ chế tồn tại chỉ để
   release-please có chỗ đứng commit. Nhưng release-please **không hề cần
   nhánh thứ hai** — thiết kế chuẩn của nó (googleapis và đa số repo dùng
   nó) là chạy trên một nhánh duy nhất: release PR mở vào chính nhánh đó,
   merge → tag + release. Mô hình 2 nhánh là lựa chọn của smind, không phải
   yêu cầu của công cụ.
3. **Copy nguyên mô hình CatchM (b) sẽ đánh mất thứ smind đang có.** Bot-tag
   thuần (validate + tag + build) không bump version trong 4 file desktop,
   không sinh CHANGELOG.md từ Conventional Commits. Với CatchM 1-maintainer
   đó là đồ thừa; với smind — public, pre-1.0 nhưng có người dùng, có desktop
   app cần version đồng bộ (`task check:versions` chạy trong CI) — bỏ nó là
   mất chức năng thật, và thói quen Conventional Commits đã ăn sâu trong
   văn hóa repo (CONTRIBUTING, toàn bộ 216 PR đều theo format).
4. **Mô hình đề xuất xóa tận gốc cả hai lỗ hổng** mà không cần lớp vá nào:
   release commit (bump + changelog) nằm ngay trên `develop` → không thể bị
   clobber, không cần sync-back, không cần check-metadata, không cần
   rebase-only ruleset. Đúng như luận điểm số 2 của CatchM: single-branch
   không có cơ hội phát sinh lớp lỗi này.

## 4. Kế hoạch chuyển đổi (nếu duyệt)

Bối cảnh thuận lợi: `master` hiện đang là ancestor của `develop` (đã
in-sync, kiểm chứng bằng `git merge-base --is-ancestor`), manifest hai
nhánh cùng 0.7.0 — không có nợ metadata khi chuyển.

1. **Quyết định (ADR hoặc discussion)**: xác nhận bỏ vai trò release-only
   của `master`; `develop` trở thành single integration branch; `master`
   giữ làm con trỏ "code đã release", fast-forward đến tag sau mỗi release
   thành công, không bao giờ là merge target. (Giữ nguyên tên `develop`
   để không phá workflow/clone của người khác; có thể cân nhắc rename
   `main` làm bước dọn dẹp riêng, không bắt buộc.)
2. **Sửa `release-please.yml`**: trigger `push: branches: [develop]`; trong
   config thêm `"target-branch": "develop"` (hoặc chạy action với default
   branch). Release PR sẽ mở vào `develop`; khi merge → tag nằm trên
   `develop` → build binaries + attach giữ nguyên như hiện nay.
3. **Thêm job fast-forward `master`** (sau job publish thành công):
   `git push origin <tag-sha>:refs/heads/master` — an toàn vì master là
   ancestor của develop; không cần quyền force-push.
4. **Xóa**: `sync-develop.yml` (toàn file), step "Guard release metadata"
   trong `ci.yml` + `scripts/check-release-metadata.sh`, điều kiện
   `github.base_ref == 'master'`.
5. **Rulesets**: gỡ ruleset `master` hiện tại (hoặc thay bằng chỉ cấm
   deletion/non-fast-forward nếu muốn master bất biến); giữ ruleset
   `develop` như cũ. Không thêm ruleset mới.
6. **Cập nhật docs**: `CONTRIBUTING.md` (branching model mới — phần lớn
   đơn giản hóa), README nếu câu chữ ám chỉ master là nhánh release.
7. **Chạy thử**: sau khi merge thay đổi, để release-please mở release PR
   cho 187 commits đang kẹt (dự kiến v0.8.0), verify: changelog đầy đủ,
   version bump đủ 4 file desktop, tag trên develop, binaries thực sự
   xuất hiện trong assets của Release (lần đầu tiên), master fast-forward
   đúng tag.

**Rủi ro + mitigations:**

- *Tag mới nằm trên `develop`, tag cũ (v0.2–v0.7) nằm trên tuyến master*:
  hai tuyến đã hội tụ tại `1c45c08` (master là ancestor develop) nên
  `git describe`/release-please so từ last tag vẫn đúng lịch sử. Không thấy
  vướng thực tế, nhưng bước 7 cần verify `compare vX..vY` render đúng.
- *Người dùng quen theo dõi `master`*: master vẫn được cập nhật (fast-forward
  theo tag) nên hành vi "master = code đã release" được bảo toàn — chính là
  ngữ nghĩa CONTRIBUTING hiện tại hứa hẹn.
- *Bỏ lỡ release PR khi maintainer merge nhầm*: release PR là PR bình thường
  trên develop, CI chạy đầy đủ; không còn cơ chế nào tự phát release ngoài
  việc merge PR đó — same-safety với mô hình hiện tại.
- *Đổi `on: push` của release-please sang develop*: mọi push vào develop sẽ
  chạy action (trước đây chỉ trên master, tần suất thấp hơn). Chi phí nhỏ
  (job đầu là action nhẹ); có thể thêm `paths-ignore` nếu cần.

**Không khuyến nghị**: giữ nguyên mô hình (pipeline đang stall — bằng chứng
mục 1.3), hay copy nguyên văn bot-tag của CatchM (mất changelog + version
sync 4 file mà smind đang dùng hằng ngày).

## 5. Dữ liệu thiếu / chưa kiểm chứng

- Chưa xác minh được tại sao README viết mục "Installing the daemon from a
  release" trước khi PR #198 (build+attach) merge — có thể README cập nhật
  cùng dòng công việc đó trong khi release thật chưa từng chạy job; ghi nhận
  thực trạng là "README hứa, Release chưa có".
- Không khảo sát thêm repo ngoài 3 dự án trong báo cáo CatchM (Syncthing,
  GoReleaser, smind); luận điểm "release-please chạy chuẩn trên single
  branch" dựa trên tài liệu/thiết kế của googleapis/release-please, không
  phải survey mới.
- Bước fast-forward master bằng `git push <sha>:refs/heads/master` giả định
  master luôn là ancestor của tag; đúng ở thời điểm viết (đã in-sync) và
  được bảo toàn về cấu trúc sau chuyển đổi, nhưng cần giữ giả định này trong
  ADR như một bất biến.
