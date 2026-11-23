# assistant-bot v1 — OCI watch module (design)

> VI: assistant-bot v1 — module canh OCI (bản thiết kế)

Date: 2026-11-23 · Status: approved by Khang (2026-11-23), handed off for implementation

> VI: Ngày 2026-11-23 · Trạng thái: Khang đã duyệt (2026-11-23), giao cho bên khác làm

## Contents / Mục lục

1. Intent — why this exists / Mục đích
2. Goals, non-goals, anti-goals / Mục tiêu, ngoài phạm vi, điều cấm
3. Context and constraints / Bối cảnh và ràng buộc
4. Approaches considered / Các cách đã cân nhắc
5. Design / Thiết kế
6. What Khang does by hand / Việc Khang tự làm
7. Left to the builder / Để người làm tự quyết

---

## 1. Intent

Khang wants a personal Discord "assistant": one private place that tells him when something on his Oracle Cloud (OCI) free-tier setup changes, and answers `/status` on demand. v1 only watches OCI. Later modules (morning brief, reminders) will plug into the same bot.

> VI: Khang muốn một "trợ lý" Discord cá nhân: một chỗ riêng báo cho Khang khi có thay đổi trên OCI free tier, và trả lời `/status` khi được hỏi. v1 chỉ canh OCI. Các module sau (bản tin sáng, nhắc việc) sẽ gắn vào cùng bot này.

Why now: a VM-side hunter (`grab-servers`) is trying to create two Micro VMs (`micro-spare`, `micro-monitor`) and capacity can appear at any time. Today the only alert is a macOS popup (`local.oci-micro-watch`, every 3 h) that works only while the Mac is awake.

> VI: Vì sao bây giờ: hunter `grab-servers` trên VM đang săn 2 VM Micro và capacity có thể xuất hiện bất cứ lúc nào. Hiện chỉ có popup trên Mac (mỗi 3 giờ), chỉ chạy khi Mac thức.

Success: Khang's phone gets a Discord message within ~5 minutes of a Micro being created, or of the VM / hunter going unhealthy, with no false alarms during normal operation.

> VI: Thành công: điện thoại Khang nhận tin Discord trong ~5 phút khi Micro được tạo, hoặc khi VM / hunter có vấn đề, và không báo nhầm khi mọi thứ bình thường.

## 2. Goals, non-goals, anti-goals

Goals (v1):

> VI: Mục tiêu (v1):

- Alert when an instance appears, disappears, or changes lifecycle state (e.g. `micro-spare` → RUNNING, `side-projects` → STOPPED).
  > VI: Báo khi một instance xuất hiện, biến mất hoặc đổi trạng thái.
- Alert when the `side-projects` VM itself is unhealthy: disk, memory, sustained CPU.
  > VI: Báo khi chính VM `side-projects` có vấn đề: đĩa, RAM, CPU cao kéo dài.
- Alert when the hunter is dead or stuck; say once when it finished normally.
  > VI: Báo khi hunter chết hoặc bị treo; báo một lần khi nó xong việc bình thường.
- Alert when the bot cannot reach the OCI API for a while (so silence never means "broken").
  > VI: Báo khi bot không gọi được OCI API một thời gian (để "im lặng" không bao giờ có nghĩa là "đang hỏng").
- Alert when the bot itself goes silent (VM down, reclaimed, hung, bot crashed, network lost) through an outside dead-man's switch (healthchecks.io), see 5.8.
  > VI: Báo khi chính bot im lặng (VM chết, bị thu hồi, treo, bot crash, mất mạng) qua dead-man's switch bên ngoài (healthchecks.io), xem 5.8.
- `/status` slash command, answered only to Khang.
  > VI: Lệnh `/status`, chỉ trả lời Khang.
- Code laid out so later modules can be added without touching the OCI module.
  > VI: Cấu trúc code cho phép thêm module sau mà không sửa module OCI.

Non-goals (v1): cost / budget tracking (needs Usage API policy, not verified yet); other modules; any web UI; multi-user support; retiring the Mac-side watcher (keep both until the bot has proven itself).

> VI: Ngoài phạm vi (v1): theo dõi chi phí/budget (cần quyền Usage API, chưa kiểm chứng); module khác; giao diện web; nhiều người dùng; gỡ watcher trên Mac (giữ cả hai đến khi bot chạy ổn).

Anti-goals — v1 fails even if it "works" if it:

> VI: Điều cấm — v1 coi như thất bại dù "chạy được" nếu:

- spams: repeats the same alert every 5 minutes, or fires on the hourly anti-idle stress job;
  > VI: spam: lặp cùng một cảnh báo mỗi 5 phút, hoặc báo nhầm vì job anti-idle chạy mỗi giờ;
- performs any write action on OCI (create, stop, delete, update);
  > VI: thực hiện bất kỳ thao tác ghi nào trên OCI (tạo, dừng, xoá, sửa);
- interferes with the hunter (shared files, restarts, API rate limits);
  > VI: làm phiền hunter (đụng file, restart, chiếm rate limit API);
- puts OCI keys on the VM, or leaks the Discord token into git or logs.
  > VI: để key OCI trên VM, hoặc lộ token Discord vào git hay log.

## 3. Context and constraints

Facts below come from `~/Downloads/knowledge-personal/general/oci-free-tier-setup-flow.md` (verified 2026-11-23) and live `oci` CLI checks on 2026-11-23.

> VI: Các sự thật dưới đây lấy từ ghi chú `oci-free-tier-setup-flow.md` (kiểm 2026-11-23) và lệnh `oci` chạy thật ngày 2026-11-23.

| Fact | Detail |
|---|---|
| Host | VM `side-projects`, `VM.Standard.A1.Flex`, 4 OCPU / 24 GB, Ubuntu 26.04 aarch64, user `ubuntu`, region `ap-singapore-1` (single AD) |
| OCI auth | Instance principal. Dynamic group `free-tier-grabbers` matches every instance in the tenancy compartment; policy `free-tier-grab` already grants `manage instance-family` → listing instances needs no new policy |
| Tenancy OCID | Instance-principal policies do not allow Identity/compartment calls; get the tenancy OCID from the instance principal provider or the metadata endpoint `http://169.254.169.254/opc/v2/instance/` (header `Authorization: Bearer Oracle`) |
| Hunter | systemd service `grab-servers`, script `/home/ubuntu/grab/grab-servers-loop-vm.sh`, log `/home/ubuntu/grab/grab.log` with a heartbeat line every 30 min. Runs 24 h per start, systemd restarts it (brief inactive gaps are normal). Exits for good when both Micros exist |
| Anti-idle job | Hourly `stress-ng` burst on this VM: CPU ~70 % for 8 min, 6 GB RAM for 10 min. Alerts must not fire on it |
| Network | Bot only needs outbound HTTPS (Discord gateway + OCI API + healthchecks.io ping). No inbound port is opened |
| Deploy path | Port 22 is blocked from the company network; deploy via OCI Cloud Shell or from home |
| Toolchain | Go 1.27 on the Mac; cross-compile `GOOS=linux GOARCH=arm64` |
| Compartments | The tenancy has no sub-compartments and `side-projects` lives in the root compartment (checked with `oci iam compartment list --compartment-id-in-subtree true`, 2026-11-23). Listing the root compartment sees every instance, including future Micros |

> VI: Bảng trên: host là VM A1 `side-projects`; xác thực OCI bằng instance principal, quyền liệt kê instance đã có sẵn; tenancy OCID lấy từ provider hoặc metadata endpoint; hunter là service `grab-servers`, log có heartbeat mỗi 30 phút, restart mỗi 24 giờ, tự thoát khi đủ 2 Micro; job anti-idle chạy mỗi giờ (CPU 70% 8 phút, RAM 6 GB 10 phút) nên không được báo nhầm; chỉ cần kết nối ra ngoài; port 22 bị chặn ở công ty nên deploy qua Cloud Shell hoặc ở nhà; build chéo từ Mac.

## 4. Approaches considered

| | Approach | Trade-off | Decision |
|---|---|---|---|
| A | Webhook only: a timer script posts one-way messages | Simplest, but no `/status` and no base for future two-way modules | Rejected |
| **B** | **One Go bot (gateway connection) with modules; runs as a systemd service on `side-projects`** | More setup (Discord app, service), but two-way and extensible | **Chosen by Khang** |
| C | Run the bot on the Mac | Dies when the Mac sleeps | Rejected |
| — | Hermes Agent (LLM agent) for monitoring | Costs tokens per check, non-deterministic, has shell on the VM | Rejected for monitoring; may be added later as a *separate* bot |

> VI: Đã cân nhắc: A chỉ webhook (đơn giản nhưng không có `/status`) – loại; **B một bot Go nhiều module chạy systemd trên VM – Khang chọn**; C chạy trên Mac – loại vì Mac ngủ là chết; Hermes – không dùng để canh vì tốn token, không chắc chắn, có quyền shell; có thể thêm sau như một bot riêng.

Also decided with Khang: one bot with many modules, not many bots (one token, one service, shared code). Split only for parts with very different risk (e.g. an AI agent).

> VI: Cũng đã chốt với Khang: một bot nhiều module, không phải nhiều bot. Chỉ tách khi phần đó khác hẳn về rủi ro (ví dụ AI agent).

## 5. Design

### 5.1 Big picture

```
side-projects VM
┌──────────────────────────────────────────────┐
│ assistant-bot (systemd, user ubuntu)          │
│                                               │
│  core: config · Discord session · owner check │
│        · alert sender · state file            │
│                                               │
│  module ociwatch (every 5 min):               │
│    instances  ── OCI API (instance principal) │
│    host       ── /proc, statfs                │
│    hunter     ── systemctl + grab.log mtime   │
│  /status  ──► reads the last snapshot         │
└──────────────┬───────────────────────────────┘
               │ outbound HTTPS only
               ▼
        Discord private server, channel #server
```

> VI: Sơ đồ: bot chạy trên VM như systemd service. Phần lõi lo cấu hình, kết nối Discord, kiểm tra chủ bot, gửi cảnh báo, file trạng thái. Module `ociwatch` mỗi 5 phút kiểm tra instance (qua OCI API), sức khoẻ máy (qua /proc), hunter (qua systemctl và log). `/status` đọc kết quả lần kiểm tra gần nhất. Chỉ có kết nối đi ra.

### 5.2 Checks (every 5 minutes)

| Check | How | Alert when | Why this rule |
|---|---|---|---|
| Instances | List instances in the tenancy, excluding TERMINATED | Name appears, disappears, or lifecycle state changes vs. last saved snapshot | Core goal: catch the Micros |
| Disk | `statfs("/")` | Used > 85 % | |
| Memory | `/proc/meminfo` `MemAvailable` | Available < 10 % (~2.4 GB) for 2 checks in a row | Anti-idle takes 6 GB (25 %) — stays clear |
| CPU | `/proc/loadavg` 5-min load ÷ CPU count | > 0.9 for 3 checks in a row (≥ 15 min) | Anti-idle is ~70 % for 8 min — stays clear |
| Hunter alive | `systemctl is-active grab-servers` | Not active for 2 checks in a row, *and* not both Micros exist | Daily restart gaps are seconds long |
| Hunter stuck | mtime of `grab.log` | Older than 45 min while service is active | Heartbeat is every 30 min |
| Hunter done | Both `micro-spare` and `micro-monitor` exist | Say once "hunter finished", stop hunter alerts | Inactive is then expected |
| OCI reachable | Instance list call errors | 3 failures in a row (≥ 15 min) | One blip is normal |

> VI: Bảng kiểm tra mỗi 5 phút: instance (báo khi xuất hiện/biến mất/đổi trạng thái); đĩa > 85%; RAM còn < 10% hai lần liên tiếp (anti-idle chỉ dùng 25% nên không chạm); CPU load/nhân > 0.9 ba lần liên tiếp (anti-idle chỉ 70% trong 8 phút nên không chạm); hunter không active hai lần liên tiếp và chưa đủ 2 Micro; hunter treo khi log không cập nhật quá 45 phút (heartbeat 30 phút); đủ 2 Micro thì báo một lần "hunter xong" rồi thôi; lỗi gọi OCI ba lần liên tiếp thì báo.

"Hunter done" is derived live on every tick, not a one-way latch (Claude's call). A Micro "exists" when its lifecycle state is anything other than TERMINATING / TERMINATED — the same rule the Mac watcher uses — so PROVISIONING already counts and the bot never calls an exited hunter "dead" while a Micro is still starting. If a Micro later disappears, the bot reports it and, because the hunter exited cleanly (systemd `Restart=on-failure` will not bring it back), says the hunter must be restarted by hand. The state file only remembers that the "hunter finished" message was sent; that flag is cleared when a Micro disappears.

> VI: "Hunter xong" được tính lại mỗi lần kiểm tra, không khoá cứng một chiều (Claude tự quyết). Micro được coi là "tồn tại" khi trạng thái khác TERMINATING/TERMINATED (giống watcher trên Mac), nên PROVISIONING cũng tính, tránh báo nhầm hunter chết khi Micro đang khởi tạo. Nếu sau đó một Micro biến mất, bot báo và nhắc phải tự khởi động lại hunter (hunter thoát bình thường nên systemd không tự bật lại). File trạng thái chỉ nhớ đã gửi tin "hunter xong" chưa; cờ này được xoá khi một Micro biến mất.

Thresholds are Go constants in v1 (no config knobs until one is needed).

> VI: Ngưỡng để là hằng số trong code ở v1, chưa cần cấu hình.

### 5.3 Alert rules (no spam)

- Every check is a tiny state machine: OK → PROBLEM sends one alert; PROBLEM → OK sends one "recovered" message. Staying in the same state sends nothing.
  > VI: Mỗi kiểm tra là một máy trạng thái nhỏ: OK → LỖI gửi một cảnh báo; LỖI → OK gửi một tin "đã ổn". Giữ nguyên trạng thái thì không gửi gì.
- Instance changes are events, not states: each change is reported once.
  > VI: Thay đổi instance là sự kiện: mỗi thay đổi báo đúng một lần.
- Instance snapshot, check states and "hunter finished" flag are saved to a small JSON state file, so a restart neither repeats old alerts nor misses a Micro created while the bot was down (it is compared with the saved snapshot on start).
  > VI: Snapshot instance, trạng thái kiểm tra và cờ "hunter xong" lưu vào file JSON nhỏ, để khi restart không báo lại tin cũ và không bỏ sót Micro được tạo lúc bot tắt.
- First run with no state file: take a baseline silently and post one "bot started" summary.
  > VI: Lần chạy đầu chưa có file trạng thái: lấy mốc im lặng và gửi một tin tóm tắt "bot đã chạy".
- Alerts mention Khang (`<@OWNER_ID>`) so the phone notifies even if the channel is muted; recoveries do not.
  > VI: Cảnh báo có tag Khang để điện thoại vẫn kêu khi kênh bị tắt thông báo; tin "đã ổn" thì không tag.
- If sending to Discord fails, log it and retry on the next tick; never drop a pending event silently.
  > VI: Gửi Discord lỗi thì ghi log và thử lại ở lần kiểm tra sau; không âm thầm bỏ sự kiện.

### 5.4 `/status`

Guild-scoped slash command (instant registration in Khang's private server). Reply is ephemeral (only Khang sees it) and built from the latest snapshot — it does not call OCI live. Any other user gets an ephemeral "not allowed" and the attempt is logged.

> VI: Lệnh slash đăng ký trong server riêng của Khang (có hiệu lực ngay). Trả lời dạng ẩn (chỉ Khang thấy), lấy từ kết quả kiểm tra gần nhất, không gọi OCI trực tiếp. Người khác gõ thì nhận "không có quyền" và bị ghi log.

Example reply:

```
side-projects · up 1d 4h · checked 2 min ago
disk 31%  mem avail 72%  load 0.12
hunter: active · last heartbeat 12 min ago
instances: side-projects RUNNING
           micro-spare   (not yet)
           micro-monitor (not yet)
OCI API: ok
```

> VI: Ví dụ trả lời: uptime VM, thời điểm kiểm tra, đĩa/RAM/load, trạng thái hunter và heartbeat, danh sách instance, tình trạng OCI API.

### 5.5 Code layout

```
cmd/assistant-bot/main.go    wiring, signal handling
internal/core/               config, Discord session, owner check, notifier, state file
internal/modules/ociwatch/   checks, state machines, /status text
deploy/assistant-bot.service systemd unit
deploy/env.example           config keys, no secrets
```

> VI: Cấu trúc: `main.go` nối các phần; `internal/core` cho phần dùng chung; `internal/modules/ociwatch` cho module canh OCI; `deploy/` chứa file systemd và mẫu cấu hình.

A module is a small Go interface (name, a run loop that receives a notifier, a status text, its slash commands). Each module runs in its own goroutine with `recover`, so one module panicking does not kill the bot; a panic is reported to Discord.

> VI: Module là một interface Go nhỏ (tên, vòng chạy nhận notifier, nội dung status, lệnh slash). Mỗi module chạy goroutine riêng có `recover`, panic của một module không làm sập bot và được báo lên Discord.

Logic that decides alerts (state machines, thresholds, instance diff, `/proc` parsing) is pure and unit-tested with fakes for OCI, Discord and the host; real OCI/Discord are exercised only in a manual smoke test.

> VI: Phần quyết định cảnh báo (máy trạng thái, ngưỡng, so sánh instance, đọc /proc) viết thuần và có unit test với bản giả của OCI, Discord và máy; OCI/Discord thật chỉ thử bằng tay khi deploy.

Libraries: `github.com/bwmarrin/discordgo`, `github.com/oracle/oci-go-sdk/v65` (instance principal provider). Standard library for everything else.

> VI: Thư viện: discordgo, oci-go-sdk (instance principal). Còn lại dùng thư viện chuẩn.

### 5.6 Config and secrets

Env file `/home/ubuntu/.config/assistant-bot/env`, mode 600, loaded by systemd `EnvironmentFile=`:

> VI: File cấu hình `/home/ubuntu/.config/assistant-bot/env`, quyền 600, systemd nạp qua `EnvironmentFile=`:

| Key | Meaning |
|---|---|
| `DISCORD_TOKEN` | Bot token (secret) |
| `DISCORD_GUILD_ID` | Khang's private server |
| `DISCORD_CHANNEL_ID` | Channel `#server` for alerts |
| `OWNER_USER_ID` | Khang's Discord user ID; the only user the bot answers |
| `HEALTHCHECK_URL` | healthchecks.io ping URL (`https://hc-ping.com/<uuid>`); treat as a secret |

> VI: Các khoá: token bot (bí mật), ID server riêng, ID kênh `#server`, ID người dùng Discord của Khang (người duy nhất bot trả lời).

The bot refuses to start if any key is missing. The token is never logged. State file: `/home/ubuntu/.local/state/assistant-bot/state.json`.

> VI: Thiếu khoá nào thì bot không khởi động. Không bao giờ ghi token ra log. File trạng thái: `/home/ubuntu/.local/state/assistant-bot/state.json`.

### 5.7 Running

systemd unit, `User=ubuntu`, `Restart=on-failure`, logs to journald. Runs as `ubuntu` because it must read `/home/ubuntu/grab/grab.log` and query `systemctl is-active` (no sudo needed for either). Expected footprint: well under 50 MB RAM, near-zero CPU.

> VI: Chạy bằng systemd dưới user `ubuntu` (cần đọc log của hunter và hỏi `systemctl is-active`, không cần sudo), tự restart khi lỗi, log vào journald. Tốn dưới 50 MB RAM, CPU gần như 0.

### 5.8 Dead-man's switch (bot or VM went silent)

The bot cannot report its own death, so an outside service watches for silence. Chosen by Khang: healthchecks.io (free plan).

> VI: Bot không tự báo được khi chính nó chết, nên dùng dịch vụ bên ngoài canh sự im lặng. Khang chọn healthchecks.io (gói miễn phí).

- At the end of every 5-minute tick the bot sends one HTTPS GET to `HEALTHCHECK_URL`. The ping proves the check loop actually ran, not just that the process exists.
  > VI: Cuối mỗi lượt kiểm tra 5 phút, bot gửi một HTTPS GET tới `HEALTHCHECK_URL`. Ping chứng minh vòng kiểm tra thật sự chạy, không chỉ là process còn sống.
- healthchecks.io check: period 5 min, grace 10 min → Khang is alerted (Discord + email, set up in healthchecks.io) after about 15 min of silence. Grace 10 min absorbs a VM reboot or a bot restart during deploy.
  > VI: Cấu hình trên healthchecks.io: chu kỳ 5 phút, ân hạn 10 phút → Khang nhận báo (Discord + email, cài trong healthchecks.io) sau khoảng 15 phút im lặng. Ân hạn 10 phút đủ cho VM reboot hay bot restart khi deploy.
- A failed ping is logged and not retried; the next tick pings again. It never blocks or delays the checks.
  > VI: Ping lỗi thì ghi log, không thử lại; lượt sau ping tiếp. Không bao giờ chặn hay làm chậm các kiểm tra.
- Cost of pinging every 5 min: 288 tiny requests a day, negligible. Detection speed is set by period + grace, not by how often the bot pings.
  > VI: Ping 5 phút một lần chỉ là 288 request nhỏ mỗi ngày, không đáng kể. Tốc độ phát hiện do chu kỳ + ân hạn quyết định, không phải tần suất ping.

## 6. What Khang does by hand

1. Create a private Discord server (if none) and a channel `#server`.
   > VI: Tạo Discord server riêng (nếu chưa có) và kênh `#server`.
2. Discord Developer Portal → New Application → Bot → copy the token. Invite the bot with scopes `bot` + `applications.commands` and permissions View Channel + Send Messages. No privileged intents are needed.
   > VI: Vào Discord Developer Portal → tạo app → Bot → lấy token. Mời bot với scope `bot` + `applications.commands`, quyền Xem kênh + Gửi tin. Không cần privileged intent.
3. Turn on Developer Mode in Discord, copy the server ID, channel ID and his own user ID.
   > VI: Bật Developer Mode trong Discord, copy ID server, ID kênh và ID người dùng của mình.
4. Create a free healthchecks.io account, add a check (period 5 min, grace 10 min), connect its Discord and email integrations, copy the ping URL.
   > VI: Tạo tài khoản healthchecks.io miễn phí, thêm một check (chu kỳ 5 phút, ân hạn 10 phút), nối Discord và email, copy URL ping.
5. Deploy from home or through OCI Cloud Shell (port 22 is blocked at work). Claude prepares the binary, unit file and step-by-step commands.
   > VI: Deploy ở nhà hoặc qua OCI Cloud Shell (port 22 bị chặn ở công ty). Claude chuẩn bị binary, file systemd và từng lệnh.

## 7. Left to the builder

Exact Go package names inside `internal/`, test layout, log format, wording of alert messages (short, English, with an emoji per severity), the deploy helper script, and how the tenancy OCID is obtained (provider vs. metadata endpoint), as long as no Identity API call is needed.

> VI: Để người làm tự quyết: tên package cụ thể, cách tổ chức test, định dạng log, câu chữ cảnh báo (ngắn, tiếng Anh, mỗi mức có emoji), script hỗ trợ deploy, và cách lấy tenancy OCID, miễn không cần gọi Identity API.
