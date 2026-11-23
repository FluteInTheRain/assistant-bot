# assistant-bot v1.1 — jobwatch module (design)

> VI: assistant-bot v1.1 — module theo dõi thị trường việc làm IT (bản thiết kế)

Date: 2026-11-23 · Status: approved by Khang (2026-11-23), implemented in the same session

> VI: Ngày 2026-11-23 · Trạng thái: Khang đã duyệt (2026-11-23), code luôn trong phiên này

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

Khang wants a daily pulse of the Vietnamese IT job market: new postings in Backend / AI / Data, with postings from finance / securities / fintech / banking companies surfaced first. The stated goal is *collecting tech-market data to process later* — so matched postings are also appended to a local archive, not just shown in a digest.

> VI: Khang muốn mỗi ngày có một bức tranh thị trường việc làm IT Việt Nam: tin mới ở Backend / AI / Data, tin của cty tài chính / chứng khoán / fintech / ngân hàng được đưa lên đầu. Mục tiêu stated là *thu thập dữ liệu thị trường công nghệ để xử lý sau* — nên các tin match còn được ghi vào kho lưu cục bộ, không chỉ hiện trong digest.

Why this shape: decisions already made with Khang (2026-11-23): one bot with one module per feature and one channel per module → this is a second module (`jobwatch`) in the existing `assistant-bot`, not a second bot. Data source: the public goodjobs.io.vn API first, with a source interface so more sources can plug in later. Digest every morning (08:00), not instant alerts. No location filter — whole country, because the goal is market data, not job hunting. `/jobs` slash command deferred.

> VI: Vì sao thế này: các quyết định đã chốt với Khang (2026-11-23): một bot, một module mỗi tính năng, một channel mỗi module → đây là module thứ hai (`jobwatch` trong `assistant-bot`), không phải bot thứ hai. Nguồn dữ liệu: API public của goodjobs.io.vn trước, kèm interface để cắm thêm nguồn sau. Digest mỗi sáng 08:00, không alert từng tin. Không lọc địa điểm — toàn quốc, vì mục tiêu là dữ liệu thị trường chứ không phải đi xin việc. Lệnh `/jobs` để sau.

Success: every morning at 08:00 the `#jobs` channel has one digest; a Khang who glances at it knows what the VN IT market did yesterday, and every matched posting since deploy exists in `jobs.jsonl` on the VM.

> VI: Thành công: mỗi sáng 08:00 kênh `#jobs` có đúng một digest; đọc lướt là biết thị trường IT VN hôm qua thế nào, và mọi tin match từ khi deploy đều nằm trong `jobs.jsonl` trên VM.

## 2. Goals, non-goals, anti-goals

Goals (v1.1):

> VI: Mục tiêu (v1.1):

- Poll goodjobs.io.vn `GET /recent-jobs` every 2 hours; find postings matching Backend / AI / Data by title and skill tags.
  > VI: Gọi API goodjobs mỗi 2 giờ; tìm tin match Backend / AI / Data theo tên-job và skill tags.
- Flag postings from finance / securities / fintech / banking companies (keyword + known-brand list) and put them in the top digest section.
  > VI: Gắn cờ tin của cty tài chính / chứng khoán / fintech / ngân hàng (theo keyword + danh sách thương hiệu biết sẵn) và đưa lên section đầu của digest.
- Daily digest at 08:00 ICT in a new `#jobs` channel — always sent, even when empty (it doubles as the module's alive-signal).
  > VI: Digest mỗi ngày 08:00 ICT vào kênh `#jobs` mới — luôn gửi, kể cả khi rỗng (đồng thời là tín hiệu "module còn sống").
- Append every matched posting (without description) to an append-only `jobs.jsonl` archive for later processing.
  > VI: Ghi mỗi tin match (không kèm mô tả dài) vào file `jobs.jsonl` kiểu append-only để xử lý sau.
- Dedup: a job never appears in two digests; a bot restart replays nothing.
  > VI: Dedup: một tin không bao giờ xuất hiện trong hai digest; restart bot không đăng lại tin cũ.
- Alert (with ping) if the goodjobs API stays unreachable ~6 h; say "recovered" once when it comes back.
  > VI: Báo (có ping) khi API goodjobs không với tới ~6 giờ; báo "đã ổn" một lần khi nối lại.
- A `jobwatch` panic must not kill `ociwatch` (existing module isolation) and is reported to Discord.
  > VI: Panic trong `jobwatch` không được làm chết `ociwatch` (cơ chế cô lập module sẵn có) và phải được báo lên Discord.

Non-goals (v1.1): the `/jobs` slash command (deferred with Khang, together with a `/status` that aggregates modules); instant per-posting alerts; location, salary or seniority filters; self-written scrapers (the interface arrives now, the scrapers later); global/remote jobs; any web UI or multi-user support.

> VI: Ngoài phạm vi (v1.1): lệnh `/jobs` (chờ chốt với Khang, cùng với `/status` tổng hợp nhiều module); alert từng tin ngay; lọc theo địa điểm, lương, cấp bậc; tự viết scraper (interface có sẵn phần này, scraper để sau); tin quốc tế/remote; giao diện web; nhiều người dùng.

Anti-goals — v1.1 fails even if it "works" if it:

> VI: Điều cấm — v1.1 coi như thất bại dù "chạy được" nếu:

- spams: the same posting twice (cross-board duplicates of one role count too), or a digest that floods the channel with hundreds of lines.
  > VI: spam: cùng một tin xuất hiện hai lần (kể cả trùng nhau giữa hai bảng tin), hay digest tràn kênh với hàng trăm dòng.
- hammers goodjobs.io.vn: retry storms, more than the one 2-hourly request, or a scraper "just in case".
  > VI: đập vào goodjobs.io.vn: retry liên hồi, vượt quá 1 request mỗi 2 giờ, hay tự scrape "cho chắc".
- loses data: a restart drops postings already collected (archive and pending-digest state survive).
  > VI: mất dữ liệu: restart làm mất tin đã thu thập (file archive và trạng thái digest phải sống sót qua restart).
- breaks the existing bot: ociwatch alerts, `/status`, or the healthcheck ping change behavior.
  > VI: hỏng phần bot sẵn có: alert của ociwatch, `/status`, hay healthcheck ping bị đổi hành vi.

## 3. Context and constraints

Live checks on 2026-11-23: `GET https://api.goodjobs.io.vn/recent-jobs` (no key) returned a JSON array of postings; the repo README documents the source list and cache behaviour.

> VI: Kiểm trực tiếp ngày 2026-11-23: `GET https://api.goodjobs.io.vn/recent-jobs` (không cần key) trả về mảng JSON các tin; README repo ghi rõ danh sách nguồn và cách hoạt động của cache.

| Fact | Detail |
|---|---|
| Host | Same VM `side-projects`, `VM.Standard.A1.Flex` 4 OCPU / 24 GB — a few-MB JSON poll every 2 h is nothing |
| Source coverage | goodjobs.io.vn merges 13+ VN boards (ITViec, TopCV, TopDev, VietnamWorks, CareerViet, JobsGo, CareerLink, ViecOi, Indeed VN, TechJobs, Xóm Jobs, Glints, LinkedIn…) |
| Upstream behaviour | Their cache is re-scraped every ~2 h; jobs pruned after 14 days; postings deduplicated by link upstream |
| Response fields (checked live) | `title`, `company` (sometimes "N/A"), `location`, `link`, `source`, `salary` (often empty), `posted`, `posted_date`, `posted_ts`, `skills` []string, `description` (long HTML — fetch-only, never stored), `_cache_key` |
| `posted_ts` is unreliable | Some sources truncate it to the day; "new" must mean *first seen by the bot*, not the timestamp |
| No SLA | goodjobs is a hobby-grade free service: could vanish or rate-limit; the `JobSource` interface exists so a replacement source can be swapped in without touching the digest logic |
| Timezone | Digest at 08:00 ICT; VN has no DST → a fixed `UTC+7` zone constant, no tzdata dependency |
| Same secrets | No new secret: reuses the existing Discord token; only a new channel ID enters the env file |

> VI: Bảng trên: vẫn chạy trên VM `side-projects` (A1.Flex 4 OCPU / 24 GB) nên polling JSON vài MB mỗi 2 giờ chẳng là gì; goodjobs gộp hơn 13 bảng tin VN; cache của họ quét lại mỗi ~2 giờ, tin cũ 14 ngày bị prune, dedup theo link phía họ; các trường trả về đã kiểm trực tiếp (đáng chú ý `description` là HTML dài — chỉ fetch, không lưu); `posted_ts` có nguồn bị cắt theo ngày nên "mới" phải hiểu là *lần đầu bot nhìn thấy*; dịch vụ miễn phí không SLA nên cần interface để thay nguồn; digest 08:00 ICT dùng hằng số UTC+7 (VN không có DST); không thêm bí mật mới — chỉ thêm ID kênh mới.

## 4. Approaches considered

| | Approach | Trade-off | Decision |
|---|---|---|---|
| A | Self-scrape 2-3 boards (ITViec/TopDev/TopCV) | Full control, but selectors rot and each board is its own maintenance project | Deferred — arrives later as another `JobSource` |
| **B** | **Poll goodjobs public API only** | One JSON GET covers 13+ boards, zero scraping code; depends on a hobby service | **Chosen by Khang** |
| C | Self-host the goodjobs stack | Full independence, but Redis + Playwright + maintenance for a personal digest | Rejected — absurd weight for one message a day |
| D | Scrape LinkedIn directly | Biggest senior market, but headless Chromium + ToS risk on a personal VM | Rejected; goodjobs already includes LinkedIn |
| — | Instant alerts per posting | Fastest awareness, but VN IT volume would make the channel unusable | Rejected by Khang — daily digest instead |

> VI: Đã cân nhắc: A tự scrape 2-3 bảng (tự chủ nhưng selector mau hỏng, mỗi bảng một gánh bảo trì) – để sau, sẽ là một `JobSource` khác; **B chỉ gọi API public của goodjobs (một GET JSON phủ 13+ bảng, không viết scraper nào; đổi lại phụ thuộc dịch vụ hobby) – Khang chọn**; C tự host nguyên stack goodjobs (độc lập hoàn toàn nhưng Redis + Playwright cho một digest cá nhân là quá nặng) – loại; D scrape LinkedIn trực tiếp (phải chạy Chromium headless, rủi ro ToS) – loại, goodjobs đã gộp LinkedIn; alert từng tin ngay (nhanh nhất nhưng lượng tin IT VN làm kênh unreadable) – Khang từ chối, lấy digest hằng ngày.

Also decided with Khang: no location filter (the goal is market data, not personal job hunting); digest at 08:00; `/jobs` command deferred out of v1.1.

> VI: Cũng đã chốt với Khang: không lọc địa điểm (mục tiêu là dữ liệu thị trường, không phải đi xin việc); digest lúc 08:00; lệnh `/jobs` không làm trong v1.1.

## 5. Design

### 5.1 Big picture

```
side-projects VM
┌───────────────────────────────────────────────────┐
│ assistant-bot (systemd, user ubuntu)              │
│                                                   │
│  module ociwatch (unchanged): every 5 min,        │
│    OCI API · host · hunter · /status · pinger     │
│                                                   │
│  module jobwatch (new):                           │
│    poll every 2 h ──► goodjobs /recent-jobs       │
│      filter  ── Backend/AI/Data keywords          │
│      dedup   ── link + (company,title) keys       │
│      archive ──► jobs.jsonl (append-only)         │
│    digest 08:00 ICT ──► channel #jobs             │
│    API down ~6 h   ──► alert (ping)               │
└──────────────────┬────────────────────────────────┘
                   │ outbound HTTPS only
                   ▼
   Discord: #server (ociwatch) · #jobs (jobwatch)
```

> VI: Sơ đồ: bot cũ chạy nguyên như trước (ociwatch 5 phút một lần, giữ nguyên `/status` và pinger). Module `jobwatch` mới: mỗi 2 giờ lấy tin từ goodjobs, lọc theo keyword Backend/AI/Data, dedup theo link và cặp (cty, tên-job), ghi tin match vào `jobs.jsonl`; 08:00 ICT gửi digest vào kênh `#jobs`; API chết ~6 tiếng thì báo có ping. Kênh `#server` của ociwatch và `#jobs` tách riêng.

### 5.2 Poll loop, dedup, archive

- One `GET /recent-jobs` every 2 h (aligned with goodjobs' own cache refresh; 12 requests/day). One attempt per tick, 30 s timeout; a failure is *not* retried in the same tick — the next tick retries. Honest User-Agent identifying the bot.
  > VI: Một GET mỗi 2 giờ (khớp chu kỳ làm mới cache của goodjobs; 12 request/ngày). Mỗi tick thử đúng một lần, timeout 30 giây; lỗi trong tick không thử lại ngay — tick sau sẽ thử lại. User-Agent ghi rõ đây là bot cá nhân.
- A posting is "new" the first time the bot sees it (first-seen semantics; `posted_ts` is not trusted).
  > VI: Một tin được coi là "mới" ở lần đầu bot nhìn thấy (không tin `posted_ts`).
- Dedup keys: the link with any `#fragment` stripped, plus `lower(company)|collapse-space(lower(title))`. Cross-board reposts of one role (different links) collapse via the second key.
  > VI: Khóa dedup: link đã bỏ phần `#fragment`, cộng thêm khóa `cty thường|tên-job thường đã gộp khoảng trắng`. Tin đăng lại trên nhiều bảng (link khác nhau) gộp nhờ khóa thứ hai.
- First run with no state file: silent baseline — mark everything seen, archive nothing, post no digest content (log line only). The first digest arrives at the next 08:00.
  > VI: Lần chạy đầu chưa có file trạng thái: lấy mốc im lặng — đánh dấu mọi tin đã thấy, không ghi archive, không đăng gì (chỉ log). Digest đầu tiên đến 08:00 kế tiếp.
- Every matched new posting → (a) one JSON line appended to `/home/ubuntu/.local/state/assistant-bot/jobs.jsonl` — fields: first_seen, link, source, title, company, location, salary, skills, match categories, finance flag; **never the description**; (b) added to the pending-digest list in the state file. The archive is append-only: pruning never touches it.
  > VI: Mỗi tin mới match → (a) một dòng JSON ghi thêm vào `jobs.jsonl` — gồm first_seen, link, nguồn, tên-job, cty, địa điểm, lương, skills, nhóm match, cờ finance; **không bao gồm mô tả dài**; (b) vào danh sách chờ digest trong file trạng thái. File archive chỉ ghi thêm, prune không đụng vào.
- Seen-set is pruned to 45 days (goodjobs keeps 14 days; 45 gives slack for a replacement source that keeps jobs longer).
  > VI: Tập "đã thấy" chỉ giữ 45 ngày (goodjobs giữ 14 ngày; 45 để dư chỗ cho nguồn khác lưu tin lâu hơn).

### 5.3 Filtering and classification (pure, unit-tested)

- Match against **title and `skills` tags only** — never the description (a bank-client project inside a JD would flag half the market).
  > VI: Chỉ match trên **tên-job và tag skills** — không đụng vào mô tả (JD có nhắc khách hàng ngân hàng là cờ cả thị trường mất).
- Keyword sets are Go constants (same v1 rule as ociwatch thresholds: no config knobs until one is needed). Word-boundary matching for short tokens (`ai`, `go`, `java`) so "ai" does not match "chairman" and "java" does not match "javascript" by accident — a `java` job is still checked as its own token.
  > VI: Bộ keyword là hằng số Go (giống rule v1 của ociwatch: chưa cần núm cấu hình). Token ngắn (`ai`, `go`, `java`) phải khớp theo ranh giới từ — "ai" không khớp trong "chairman", "java" không khớp trong "javascript" nhờ so token riêng.
- Backend set (draft, builder may refine wording): `backend / back-end / golang / go / python / node / java / .net / c# / php / laravel / spring / rust / c++ / kotlin`.
  > VI: Bộ Backend (bản nháp, người làm có thể chỉnh câu chữ): `backend / back-end / golang / go / python / node / java / .net / c# / php / laravel / spring / rust / c++ / kotlin`.
- AI · Data set: `ai / machine learning / deep learning / llm / nlp / computer vision / data engineer / data science / data analyst / analytics / big data / mlops / tensorflow / pytorch`.
  > VI: Bộ AI · Data: `ai / machine learning / deep learning / llm / nlp / computer vision / data engineer / data science / data analyst / analytics / big data / mlops / tensorflow / pytorch`.
- Finance flag — generic terms on company+title: `bank / banking / ngân hàng / chứng khoán / securities / fintech / finance / tài chính / fund / quỹ / đầu tư / investment / capital / forex / crypto`; plus a short known-brand list (momo, vnpay, zalopay, moca, techcombank, vpbank, tpbank, vietcombank, bidv, vietinbank, sacombank, mb bank, hdbank, ssi, vndirect, vietcap, tcbs, f88). Brands are whole-token matched against company **and title** (ITviec often reports company as "N/A" with the name embedded in the title), never against skills.
  > VI: Cờ finance — thuật ngữ chung trên cty+tên-job: `bank / banking / ngân hàng / chứng khoán / securities / fintech / finance / tài chính / fund / quỹ / đầu tư / investment / capital / forex / crypto`; cộng danh sách ngắn thương hiệu biết sẵn (momo, vnpay, zalopay, moca, techcombank, vpbank, tpbank, vietcombank, bidv, vietinbank, sacombank, mb bank, hdbank, ssi, vndirect, vietcap, tcbs, f88). So thương hiệu khớp nguyên token trên cty **và tên-job** (ITviec hay trả cty là "N/A" rồi nhét tên vào tên-job), không áp trên skills.
- A posting matches the digest if it hits the Backend or AI·Data set. The finance flag then decides its section.
  > VI: Một tin lọt vào digest khi trúng bộ Backend hoặc AI·Data. Cờ finance quyết định nó nằm section nào.

### 5.4 The digest (08:00 ICT daily)

- One digest per day, **always sent** — even "0 new matched postings" — because the digest *is* the module's alive-signal; a missing digest means something is wrong. Not an alert: no ping.
  > VI: Mỗi ngày đúng một digest, **luôn gửi** — kể cả "0 tin mới" — vì digest chính là tín hiệu module còn sống; digest không đến nghĩa là có vấn đề. Không phải alert nên không ping.
- Sections, in order: 🔥 Finance · Fintech (matched postings with the finance flag) → 💼 Backend → 🤖 AI · Data. A posting appears in exactly one section.
  > VI: Thứ tự section: 🔥 Finance · Fintech (tin match có cờ finance) → 💼 Backend → 🤖 AI · Data. Một tin chỉ xuất hiện đúng một section.
- Per posting, one line: `title — company — location — salary(if any) — source — link`.
  > VI: Mỗi tin một dòng: `tên-job — cty — địa điểm — lương (nếu có) — nguồn — link`.
- Caps: 30 postings per digest, `+N more` footnote (the archive has everything). Digest is chunked into ≤1900-char Discord messages; it is one Info send per chunk, sequential.
  > VI: Giới hạn: 30 tin mỗi digest, chú thích `+N tin khác` (archive đã có đủ). Digest cắt thành các tin nhắn Discord ≤1900 ký tự, gửi tuần tự, mỗi phần một Info.
- Header: date, window (time since previous digest), counts per section, and one footer line: API state + last successful poll age. With the API down it also says so.
  > VI: Đầu digest: ngày, khoảng thời gian (từ digest trước), số tin mỗi section; dòng cuối: tình trạng API và lần poll thành công gần nhất. API đang chết thì ghi rõ.

### 5.5 No-spam and failure rules

- API health is a state machine like every ociwatch check: 3 consecutive failed polls (~6 h) → one Alert (ping) "goodjobs unreachable ~6 h"; recovery → one Info "API back". Steady state sends nothing.
  > VI: Sức khoẻ API là một máy trạng thái như mọi check của ociwatch: 3 lần poll thất bại liên tiếp (~6 giờ) → một Alert có ping "goodjobs không với tới ~6 giờ"; hồi phục → một Info "API đã quay lại". Trạng thái ổn định không gửi gì.
- A digest is never repeated: the pending list is cleared from state *after* the digest is queued to the notifier, and the state file is saved before sending (crash between the two at worst skips one window into the next digest — postings are already in the archive; nothing is lost).
  > VI: Không gửi lại digest nào: danh sách chờ được xoá khỏi trạng thái *sau* khi digest đã vào queue gửi, và file trạng thái lưu trước khi gửi (crash giữa hai bước tệ nhất là dồn một khung vào digest sau — tin vẫn nằm trong archive, không mất gì).
- Restart: the seen-set and pending list persist; the first tick after a restart posts no catch-up — new postings found in the next poll flow into the next digest normally.
  > VI: Restart: tập đã-thấy và danh sách chờ được giữ; tick đầu sau restart không đăng bù gì — tin mới từ lần poll kế tiếp chảy vào digest kế tiếp bình thường.
- A panic in `jobwatch` is caught by the existing per-module `recover`: the bot keeps running ociwatch, and the panic lands in `#jobs` as an Alert.
  > VI: Panic trong `jobwatch` bị `recover` sẵn có bắt giữ: bot vẫn chạy ociwatch, và tin panic vào kênh `#jobs` như một Alert.
- Known hole, accepted in v1.1: a *hung* (not panicked) jobwatch goroutine is flagged only by a missing digest; the healthchecks.io dead-man's switch stays wired to ociwatch's 5-min loop. A second healthchecks.io check for jobwatch is the v2 fix if this ever bites.
  > VI: Lỗ hổng đã biết, chấp nhận ở v1.1: jobwatch bị *treo* (không panic) chỉ phát hiện qua digest không đến; dead-man's switch healthchecks.io vẫn nối với vòng 5 phút của ociwatch. Tạo thêm check healthchecks.io thứ hai cho jobwatch là cách vá ở v2 nếu cần.

### 5.6 Code layout, config, state

```
internal/modules/jobwatch/
    module.go       wiring: poll loop + digest timer, core.Module impl
    goodjobs.go     JobSource implementation over GET /recent-jobs
    source.go       JobSource interface (Fetch(ctx) ([]Job, error))
    match.go        keyword sets, classifier (pure)
    match_test.go   classifier + dedup-key tests
    digest.go       digest building + chunking (pure)
    digest_test.go
    state.go        seen-set, pending list, prune, JSONL append
cmd/assistant-bot/main.go   + jobwatch.New(Deps{...}) in the module list
```

> VI: Bố cục: package `jobwatch` gồm module wiring, JobSource qua GET /recent-jobs, interface `JobSource` (cái "cắm thêm dần" của Khang), bộ lọc thuần + test, phần dựng digest thuần + test, phần trạng thái + ghi JSONL. `main.go` chỉ thêm dòng đăng ký module.

- New env key (required, same no-defaults rule): `JOBWATCH_CHANNEL_ID` — the `#jobs` channel. The existing `#server` key and everything else are untouched.
  > VI: Thêm khoá env bắt buộc (vẫn theo luật không-mặc-định): `JOBWATCH_CHANNEL_ID` — kênh `#jobs`. Khoá `#server` hiện có và mọi thứ khác giữ nguyên.
- `main.go` builds a second `core.Notifier` bound to `#jobs` and hands it to jobwatch (the Notifier is already channel-parameterized; no core change beyond wiring).
  > VI: `main.go` dựng thêm một `core.Notifier` thứ hai trỏ vào `#jobs` rồi đưa cho jobwatch (Notifier vốn đã nhận channel qua tham số; phần lõi chỉ thêm wiring).
- State: `/home/ubuntu/.local/state/assistant-bot/jobwatch-state.json` (separate file, next to ociwatch's) + archive `/home/ubuntu/.local/state/assistant-bot/jobs.jsonl`.
  > VI: Trạng thái: `jobwatch-state.json` (file riêng, cạnh file của ociwatch) + archive `jobs.jsonl`.
- jobwatch implements `StatusText()` (required by the Module interface) but nothing surfaces it in v1.1 — `/status` aggregation across modules is deliberately bundled with the deferred `/jobs` command.
  > VI: jobwatch vẫn hiện thực `StatusText()` (interface Module bắt buộc) nhưng v1.1 chưa hiển thị ở đâu — việc `/status` gộp nhiều module chủ để chung với lệnh `/jobs` bị hoãn.

### 5.7 Running

Same systemd unit, same env file, one more channel. Footprint: one 2-hourly JSON GET (response can be a few MB) and one daily message — well under 10 MB RAM added, near-zero CPU. Digest timing uses a fixed `UTC+7` zone constant (no tzdata dependency).

> VI: Vẫn cùng unit systemd, cùng file env, thêm một kênh. Tài nguyên: một GET JSON mỗi 2 giờ (response có thể vài MB) và một tin nhắn mỗi ngày — cộng dưới ~10 MB RAM, CPU gần 0. Giờ digest dùng hằng số múi `UTC+7` cố định (không phụ thuộc tzdata).

## 6. What Khang does by hand

1. In the private server, create a channel `#jobs` (or any name); copy its channel ID (Developer Mode → right-click channel → Copy Channel ID).
   > VI: Trong server riêng, tạo kênh `#jobs` (tên gì cũng được); copy ID kênh (Developer Mode → chuột phải kênh → Copy Channel ID).
2. Add `JOBWATCH_CHANNEL_ID=` to `/home/ubuntu/.config/assistant-bot/env` and redeploy the binary (existing steps in `deploy/DEPLOY.md`).
   > VI: Thêm `JOBWATCH_CHANNEL_ID=` vào file env trên VM và deploy lại binary theo đúng các bước sẵn có trong `deploy/DEPLOY.md`.

Nothing else: no new token, no new healthchecks.io check, no other server.

> VI: Ngoài ra không cần làm gì: không token mới, không check healthchecks.io mới, không server khác.

## 7. Left to the builder

Exact keyword wording beyond the draft sets above (adding or trimming a few terms is fine), digest emoji and line wording, JSONL field order, the honest User-Agent string, how the 08:00 timer is scheduled (next-fire calculation vs. a minute ticker), and test file layout — as long as the match/dedup/digest logic stays pure and unit-tested with a fake `JobSource`, per the v1 pattern.

> VI: Để người làm tự quyết: câu chữ keyword ngoài các bộ nháp trên (thêm bớt vài từ được), emoji và câu chữ digest, thứ tự trường JSONL, chuỗi User-Agent trung thực, cách tính giờ 08:00 (tính lần chạy kế tiếp hay ticker từng phút), bố cục file test — miễn là phần lọc/dedup/digest thuần và có unit test với `JobSource` giả, đúng pattern v1.
