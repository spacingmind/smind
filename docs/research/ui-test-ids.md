# Research: data-testid coverage for `web/packages/ui` (Playwright e2e readiness)

Status: research only — no source files were modified while producing this report.
Scope: `web/packages/ui/src` (React + Vite, Testing Library/jsdom for unit tests today; target is Playwright e2e).

## 1. Inventory hiện trạng

### 1.1 Đã có `data-testid` tốt (giữ nguyên, dùng làm mẫu tham khảo)

**`components/diff-viewer-pane.tsx`** — instrumented tốt nhất trong repo, dùng dynamic path-scoped ids:
- `diff-viewer-pane` (root, :156), `diff-file-list` (:164), `diff-error` (:166), `diff-empty` (:172)
- `commit-bar` (:197), `commit-success` (:199), `commit-error` (:205), `commit-message` (:215, textarea), `commit-button` (:217)
- Per-file dynamic: `` diff-file-${path} `` (:262), `` stage-${path} `` (:270, có kèm `aria-label="Stage ${path}"`), `` diff-file-header-${path} `` (:276), `` viewed-${path} `` (:285, không có aria-label), `` diff-container-${path} `` (:297)

**`components/terminal-pane.tsx`**:
- `terminal-status` (:346), `connection-banner` (:361), `terminal-ended` (:366), `terminal-error` (:371), `terminal-container` (:375, xterm.js mount)

**`components/task-detail.tsx`**:
- `connection-banner` (:46), `run-entry` + `data-run-id` (:99), `run-status` (:103), `run-text` (:121), `pending-permission` (:173)

**`components/file-editor-pane.tsx`**:
- `file-editor-pane` (:218, root), `file-editor-path` (:220), `preview-toggle` (:226), `file-conflict-banner` (:262), `conflict-reload` (:276), `conflict-overwrite` (:284)

**`components/file-preview.tsx`**:
- `markdown-preview` (:39), `svg-preview` (:52), `html-preview-frame` (:78), `preview-empty` (:111)

**`components/code-mirror-editor.tsx`**: mount `<div>` takes a `testId` prop from its caller (:104) — reusable pattern, not a fixed value.

### 1.2 Có test id nhưng generic/không unique (cần bổ sung `data-*` phụ trợ hoặc ghi chú dùng compound selector)

- **`components/file-explorer-pane.tsx`**: `file-explorer-pane` (root, :30); mọi hàng file/dir dùng chung `data-testid="file-row"` hoặc `"dir-row"` (`TreeRow`, :155) **nhưng có kèm `data-path={dataPath}`** (:156) → Playwright vẫn target được qua `[data-testid="file-row"][data-path="..."]`. Đủ dùng, không cần đổi.
- **`components/folder-picker-dialog.tsx`**: `folder-row` (:127) và `git-repo-indicator` (:139) — **không có `data-path` đi kèm** như file-explorer, nên không thể target một hàng cụ thể trong Playwright. Cần bổ sung `data-path` (không nhất thiết phải đổi testid).
- **`components/app-sidebar.tsx`**: chỉ có `task-attention` (:638, kèm `aria-label="task needs attention"`) — điểm sáng duy nhất trong file này.

### 1.3 Hoàn toàn chưa có test id (gap lớn nhất)

**`components/crud-dialogs.tsx`** — 0 testid. Chứa `CreateWorkspaceDialog`, `CreateSpaceDialog`, `CreateTaskDialog`, `DeleteWorkspaceDialog`, `DeleteSpaceDialog`, `ArchiveTaskDialog`. Cancel/Submit dùng chung component `FormActions` (:42-47) — cấu trúc **giống hệt nhau giữa mọi dialog**, chỉ khác text hiển thị ("Creating…"/"Deleting…"/"Archiving…"). Đây là gap nghiêm trọng nhất vì Playwright hiện chỉ có thể phân biệt các dialog qua text tiêu đề hoặc thứ tự DOM.

**`components/accounts-dialog.tsx`** — 0 testid. OAuth connect buttons (:184, lặp theo `OAUTH_PROVIDERS`) chỉ phân biệt qua text ("Connect Anthropic (Claude)" / "Connect OpenAI (Codex)"); manual credential form dùng `id` (không phải `data-testid`) cho input/select/textarea.

**`App.tsx`** — 0 testid. Chứa: connection status header (`<span>{STATUS_LABEL[...]}</span>`, :130-132), toàn bộ Tabs Chat/Files/Diff/Terminal (`TabsTrigger`, :146-162 — chỉ có `data-slot="tabs-trigger"` từ Radix, không phải testid), tab close "×" (`role="button"` + `aria-label="Close ${title}"`, :148-160), empty state "Select a task to get started." (:172-175).

**`components/app-sidebar.tsx`** — phần lớn không có testid:
- "Accounts settings" gear icon (:224-231, có `aria-label` ổn định — giữ nguyên, không cần testid)
- "New workspace" icon button ở header (:240-248, `aria-label="New workspace"` — ổn định, cân nhắc thêm testid nếu Playwright cần phân biệt icon này với nút "New workspace" trong empty-state)
- Empty-state onboarding block khi `workspaces.length === 0` (:258-273), gồm nút "New workspace" (:269) — không có testid/aria-label
- Workspace row (`SidebarMenuButton`, :447-451) — không có testid
- **Row actions trigger dùng chung `aria-label="Row actions"` cho cả workspace row (:396, qua `RowMenu`) lẫn space row (:536-543)** — không unique, cần phân biệt
- Space row (`SidebarMenuSubButton`, :579-583) — không có testid
- Task row (`SidebarMenuSubButton`, :633-646) — không có testid; task status text (:643-645) — không có testid
- Task actions trigger (:650-652) — `aria-label={\`Actions for ${task.Title}\`}` — **động theo tên task, không ổn định nếu task được đổi tên**

**`task-detail.tsx`** — Send button (:269-271, chỉ có text "Send", không aria-label/testid); Provider select có `aria-label="Provider"` (ổn định, giữ nguyên); Prompt input có `aria-label="Prompt"` (:262-268, ổn định, giữ nguyên); Stop button (:107-116) và permission-option buttons (:177-187) không có testid/aria-label ổn định.

### 1.4 Shared primitives (`components/ui/*`) — KHÔNG gắn testid trực tiếp vào đây

`button.tsx`, `dialog.tsx`, `dropdown-menu.tsx`, `input.tsx`, `select.tsx`, `sidebar.tsx`, `tabs.tsx`, `scroll-area.tsx`, `separator.tsx`, `resizable.tsx` (+ `sheet.tsx`, `skeleton.tsx`, `tooltip.tsx` chưa được dùng ở 8 file khảo sát) — tất cả build trên `radix-ui` + `class-variance-authority`, dùng chung bởi nhiều feature khác nhau. Gắn testid cố định ở tầng này sẽ khiến mọi instance có cùng 1 giá trị → vô dụng cho Playwright. Xem mục 4.

## 2. Convention đề xuất

Không tìm thấy tài liệu convention viết sẵn trong `refs/paseo` (đã kiểm tra `docs/testing.md`, `docs/mobile-testing.md`, `CLAUDE.md`, `CONTRIBUTING.md`) — convention ở đó là "emergent" qua 378 lần dùng `data-testid`/`testID`, quan sát được pattern sau:

- **kebab-case, toàn chữ thường**, cấu trúc `<region>-<element>[-<role>]`:
  - Static: `sidebar-settings`, `command-center-panel`, `command-center-input`
  - Có role suffix: `rename-modal-input`, `rename-modal-submit`, `rename-modal-cancel`, `root-error-boundary-retry`
- **Composition qua prop `testID` lan truyền xuống con**: cha nhận `testID` (mặc định vd `"assistant-fork-menu"`), con tự suffix bằng template literal: `` `${testID}-trigger` ``, `` `${testID}-content` ``. Đây là pattern tái sử dụng chủ đạo, không phải style guide viết tay.
- **Dynamic/entity-scoped ids**: nối id động bằng `-` hoặc `_` (dùng `_` để phân biệt phần tĩnh kebab-case với phần động, vd `workspace-tab-agent_${agentId}`), hoặc dùng `:` cho composite key (`${serverId}:${workspaceId}`). Ví dụ thực tế trong Playwright specs: `sidebar-workspace-row-${serverId}:${workspaceId}`, `sidebar-workspace-kebab-${serverId}:${workspaceId}`.

**Đề xuất cho smind** (kế thừa pattern trên, đơn giản hoá cho quy mô hiện tại):

1. Cấu trúc: `<region>-<element>[-<role>]`, ví dụ: `sidebar-workspace-row`, `dialog-new-workspace-path-input`, `chat-send-button`.
2. Với danh sách lặp (workspace/space/task rows, dialog theo path...), **ưu tiên testid tĩnh + `data-*-id` phụ trợ** thay vì nhúng ID động vào chuỗi testid (theo đúng pattern `file-explorer-pane.tsx` đã dùng: `data-testid="file-row" data-path="..."`). Lý do: selector Playwright đơn giản hơn (`page.getByTestId('sidebar-workspace-row').filter({has: ...})` hoặc CSS attribute selector), tránh phải escape ký tự đặc biệt trong ID (path, uuid) khi nhúng vào testid string. Chỉ dùng cách nhúng ID vào testid (kiểu paseo) khi cần lấy đúng 1 phần tử bằng `getByTestId` duy nhất mà không thể lọc thêm.
3. Region prefix chuẩn hoá theo khu vực UI: `sidebar-`, `dialog-<action>-`, `chat-`, `workspace-tab-`, `app-`.
4. Role suffix chuẩn hoá: `-row`, `-trigger`, `-input`, `-button`, `-submit`, `-cancel`, `-confirm`, `-status`, `-banner`.
5. Dialog CRUD: đặt tên theo hành động cụ thể (`dialog-new-workspace-*`, `dialog-add-task-*`, `dialog-add-space-*`, `dialog-delete-workspace-*`, `dialog-delete-space-*`, `dialog-archive-task-*`) để tránh đụng độ giữa các dialog dùng chung `FormActions`/`Dialog` primitive nhưng có nội dung khác nhau.
6. Gắn testid ở **call site (tầng feature)**, không sửa primitive dùng chung — xem mục 4.

## 3. Danh sách cụ thể cần gắn

| # | Element | Test id đề xuất | File:line | Lý do / e2e flow |
|---|---|---|---|---|
| 1 | "Accounts settings" gear icon | *(giữ `aria-label="Accounts settings"`, không cần testid)* | app-sidebar.tsx:224 | label đã ổn định, unique |
| 2 | "New workspace" icon button (sidebar header) | `sidebar-new-workspace-button` | app-sidebar.tsx:240 | mở dialog tạo workspace — flow chính, cần phân biệt với nút cùng tên ở empty-state |
| 3 | Empty-state "New workspace" button | `sidebar-empty-new-workspace-button` | app-sidebar.tsx:269 | flow onboarding lần đầu (chưa có workspace nào) |
| 4 | Workspace row | `sidebar-workspace-row` + `data-workspace-id` | app-sidebar.tsx:447 | chọn/mở workspace trong e2e |
| 5 | Workspace row actions trigger | `sidebar-workspace-actions-trigger` + `data-workspace-id` | app-sidebar.tsx:452 (RowMenu impl :396) | hiện dùng chung `aria-label="Row actions"` với space row → không unique, không filter được bằng role/label |
| 6 | Space row | `sidebar-space-row` + `data-space-id` | app-sidebar.tsx:579 | chọn/mở space (hoặc "Ungrouped") |
| 7 | Space row actions trigger | `sidebar-space-actions-trigger` + `data-space-id` | app-sidebar.tsx:536 | flow Add task / Delete space từ space row — cùng lý do #5 |
| 8 | Task row | `sidebar-task-row` + `data-task-id` | app-sidebar.tsx:633 | chọn task để mở chat — flow trung tâm nhất của app |
| 9 | Task row status text | `sidebar-task-status` | app-sidebar.tsx:643 | assert RUNNING/DONE/ERROR sau khi gửi prompt |
| 10 | Task actions trigger | `sidebar-task-actions-trigger` + `data-task-id` | app-sidebar.tsx:650 | `aria-label` hiện tại (`Actions for ${task.Title}`) đổi theo tên task → không ổn định nếu rename |
| 11 | "Archive task" menu item | `sidebar-task-archive-action` | app-sidebar.tsx:655 | flow archive task |
| 12 | New workspace dialog — path input | `dialog-new-workspace-path-input` | crud-dialogs.tsx:144 | flow tạo workspace (đã có `id="workspace-path"`, có thể thêm testid song song) |
| 13 | New workspace dialog — Browse button | `dialog-new-workspace-browse-button` | crud-dialogs.tsx:152 | mở `FolderPickerDialog` |
| 14 | New workspace dialog — title input | `dialog-new-workspace-title-input` | crud-dialogs.tsx:161 | optional title field |
| 15 | New workspace dialog — submit | `dialog-new-workspace-submit` | crud-dialogs.tsx:45 (shared `FormActions`, dialog context tại :110) | Cancel/Submit dùng chung component, cần testid theo context dialog cụ thể, không phải theo `FormActions` |
| 16 | New workspace dialog — cancel | `dialog-new-workspace-cancel` | crud-dialogs.tsx:42 (context :110) | cùng lý do #15 |
| 17 | New space dialog — title input | `dialog-new-space-title-input` | crud-dialogs.tsx:226 | flow tạo space |
| 18 | New space dialog — submit/cancel | `dialog-new-space-submit` / `dialog-new-space-cancel` | crud-dialogs.tsx:182-238 | cùng lý do #15 |
| 19 | New task dialog — title input | `dialog-new-task-title-input` | crud-dialogs.tsx:295 | flow tạo task |
| 20 | New task dialog — space select | `dialog-new-task-space-select` | crud-dialogs.tsx:307 | chọn space khi tạo task (ẩn nếu có `fixedSpaceId`) |
| 21 | New task dialog — submit/cancel | `dialog-new-task-submit` / `dialog-new-task-cancel` | crud-dialogs.tsx:240-327 | cùng lý do #15 |
| 22 | Delete workspace — confirm button | `dialog-delete-workspace-confirm` | crud-dialogs.tsx:381 | flow xoá workspace — cần phân biệt với confirm của delete space/archive task |
| 23 | Delete space — confirm button | `dialog-delete-space-confirm` | crud-dialogs.tsx:407-467 | flow xoá space |
| 24 | Archive task — confirm button | `dialog-archive-task-confirm` | crud-dialogs.tsx:469-529 | flow archive task (dialog xác nhận, khác với menu item #11) |
| 25 | Folder picker — row | *(giữ `folder-row`, bổ sung `data-path`)* | folder-picker-dialog.tsx:127 | thiếu `data-path` nên không target được 1 hàng cụ thể, khác với file-explorer đã có |
| 26 | Chat prompt input | *(giữ `aria-label="Prompt"`, không cần testid)* | task-detail.tsx:262 | role/label đã ổn định và unique — ưu tiên theo Testing Library |
| 27 | Send button | `chat-send-button` | task-detail.tsx:269 | không có aria-label/text ổn định để phân biệt (chỉ có text "Send") |
| 28 | Provider select | *(giữ `aria-label="Provider"`, không cần testid)* | task-detail.tsx:249 | đã ổn định |
| 29 | Stop button (đang chạy run) | `chat-stop-button` | task-detail.tsx:107 | flow dừng 1 run đang chạy |
| 30 | Permission option buttons | `` chat-permission-option-${index} `` | task-detail.tsx:177 | label động theo `option.label`, cần id ổn định để click đúng option trong e2e |
| 31 | Tabs Chat/Files/Diff/Terminal | `` workspace-tab-${kind} `` (kind ∈ task/files/diff/terminal) | App.tsx:146 (kind vocab tại tab-registry.tsx:34-39) | chuyển tab — flow lõi của toàn bộ UI |
| 32 | Tab close ("×") | `workspace-tab-close` + `data-tab-key` | App.tsx:148 | đóng file tab đang mở |
| 33 | App-level connection status | `app-connection-status` | App.tsx:130 | assert trạng thái kết nối daemon (Connecting/Connected/Reconnecting/Disconnected) |
| 34 | App empty state ("Select a task…") | `app-empty-state` | App.tsx:172 | assert trạng thái chưa chọn task |
| 35 | Accounts dialog — OAuth connect button | `` accounts-connect-${provider} `` | accounts-dialog.tsx:184 | phân biệt nút Connect theo provider (Anthropic/OpenAI...), hiện chỉ phân biệt qua text |
| 36 | Accounts dialog — manual form toggle | `accounts-manual-toggle` | accounts-dialog.tsx:224 | đã có `aria-expanded`, có thể giữ nguyên; thêm testid nếu cần click trực tiếp không qua text |
| 37 | Accounts dialog — Add account submit | `accounts-add-submit` | accounts-dialog.tsx:282 | flow thêm credential thủ công |
| 38 | Sidebar workspace row's expand/collapse chevron | *(không cần testid riêng — click cả row #4 đã đủ)* | app-sidebar.tsx:447 | ghi chú, không thêm mới |

**Tổng: 38 dòng** trong bảng — trong đó **~5 dòng là "giữ nguyên, không cần đổi"** (đã có aria-label/testid đủ tốt) và **~33 dòng là đề xuất thêm/bổ sung mới**.

## 4. Lưu ý

- **role/aria-label vs data-testid**: theo khuyến nghị Testing Library, ưu tiên `getByRole`/`getByLabelText` khi label ổn định và duy nhất. Các trường hợp nên **giữ nguyên aria-label, không thêm data-testid**: `aria-label="Prompt"` (task-detail.tsx:262), `aria-label="Provider"` (:249), `aria-label="Accounts settings"` (app-sidebar.tsx:224), `aria-label="New workspace"` (app-sidebar.tsx:240, nhưng cân nhắc thêm testid nếu cần phân biệt với nút cùng tên ở empty-state — xem #2/#3). Chỉ thêm `data-testid` khi:
  1. Label/text trùng lặp giữa nhiều instance cùng lúc trên DOM (vd `aria-label="Row actions"` dùng chung cho workspace row và space row — #5/#7).
  2. Label chứa nội dung động dễ đổi (vd `aria-label={Actions for ${task.Title}}` — #10).
  3. Element không có text/label ổn định, chỉ có text hiển thị trùng với nhiều component khác (vd nút "Submit"/"Cancel" lặp lại y hệt giữa 6 dialog trong crud-dialogs.tsx — #15-24).
- **Tránh gắn vào `components/ui/*`** (Button, Dialog, DropdownMenu, Input, Select, Tabs, Sidebar...): đây là primitive dùng chung bởi nhiều feature, gắn testid cố định ở đây sẽ tạo ra giá trị trùng lặp ở mọi nơi sử dụng. Thay vào đó, gắn `data-testid` tại **call site** — nơi feature component render primitive đó — đúng theo cách `diff-viewer-pane.tsx` đang làm (mục 1.1). Với các primitive không nhận `data-testid`/`className` pass-through trực tiếp (vd một số Radix wrapper), có thể cần kiểm tra xem component đã forward `...props` xuống DOM node chưa trước khi gắn (không thuộc phạm vi báo cáo này để verify chi tiết — cần rà lại khi thực thi).
- **Danh sách động** (workspace/space/task rows, dialog theo path): ưu tiên testid tĩnh + `data-*-id` phụ trợ (mục 2, điểm 2) thay vì nhúng ID vào chuỗi testid, trừ khi cần `getByTestId` unique tuyệt đối cho 1 phần tử.
- `refs/paseo` không có tài liệu convention chính thức cho test id — convention ở đó hình thành qua thực hành nhất quán (`testID` prop lan truyền + suffix). Báo cáo này là tài liệu convention đầu tiên viết ra cho phần UI của smind; nên cân nhắc lưu lại thành quy ước chính thức (vd trong CLAUDE.md hoặc docs/testing) khi bắt đầu triển khai Playwright.
- Phạm vi báo cáo giới hạn ở 8 file được yêu cầu + các gap nổi bật phát hiện thêm (`folder-picker-dialog.tsx`). Các pane khác trong `web/packages/ui/src/components/` (đã có test id tương đối đầy đủ — mục 1.1) không được liệt kê lại trong bảng mục 3 để tránh trùng lặp, nhưng đã ghi nhận đầy đủ ở mục 1.
