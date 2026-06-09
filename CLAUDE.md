# CLAUDE.md

本檔供 Claude Code 與後續維護者快速理解本專案的架構、規範與不可破壞的不變量。
人類導向的說明文件請見 [README.md](README.md) 與 [docs/](docs/)；本檔聚焦「改動程式碼前必須知道的事」。

## 專案是什麼

`stockbot-long-backend` 是以 Go 撰寫的台股 ETF 長線與波段交易後端。它回補 TWSE 歷史價量、
執行牛熊 regime 感知的現金比例加減碼策略、保存投資組合狀態，並提供 REST API 與 Prometheus metrics。
預設追蹤 `00631L`（2x 槓桿）與 `00830`。部署為 Go app + MariaDB + Caddy 的 Docker Compose。

## 常用命令

```bash
go build ./...            # 編譯全部套件
go test ./...             # 執行全專案測試（提交前必跑）
go vet ./...              # 靜態檢查（CI 會跑）
gofmt -l .               # 列出未格式化檔案（應為空）

go run ./cmd/server      # 啟動正式 server + 上線交易 loop
go run ./cmd/fetch_data  # 下載 TWSE 歷史資料到 data/*.csv
go run ./cmd/eval_csv    # 用 CSV 跑全期 + walk-forward + IS/OOS 評估
go run ./cmd/sweep       # 對策略旋鈕暴力網格搜尋 (四關卡過濾 + Calmar 排序 + OOS 護欄;不改 config)
go run ./cmd/evaluate    # 用 DB 資料跑 walk-forward 評估
go run ./cmd/research_run# 用 DB 資料跑單一研究回測
go run ./cmd/db_probe    # 檢查 DB schema / table / 筆數

docker compose up -d     # 正式機部署（拉 GHCR image；升級用 docker compose pull）
```

提交前的最低門檻：`gofmt -l .` 為空、`go vet ./...` 與 `go test ./...` 全綠。

## 架構與分層

採命令式外殼 + 純核心（imperative shell / functional core）的分層設計，依賴方向由外向內單向流動：

```
cmd/*            程式進入點（server 與各 CLI 工具）
  └─ internal/server      Echo 組裝：middleware chain、route 註冊、Start
       └─ internal/controller   Echo handler 的業務入口（呼叫 service，組 DTO）
            └─ internal/service      商業邏輯協調（I/O orchestration）
                 ├─ internal/service/trading    純記憶體交易引擎與買賣決策（零 I/O）
                 ├─ internal/service/backtest    回測、walk-forward、績效指標、CSV 載入
                 ├─ internal/repository          MariaDB CRUD/查詢
                 ├─ internal/client/twse         TWSE 行情 client
                 └─ internal/client/discord      Discord 通知 client
                      └─ internal/platform/mariadb   連線池與 schema 初始化
```

支援套件：`internal/config`（讀 `config.yaml` 與 per-stock override）、`internal/dto`（API 回應型別）、
`internal/entity`（DB 實體）、`internal/handler`（health/ready）、`internal/middleware`（log/metrics）、
`internal/logging`（logrus）、`helper`（小工具）。

依賴注入一律用 constructor wiring（`NewXxx(...)`），介面定義在使用端（`internal/service/ports.go`）。

### 啟動流程（cmd/server）

1. `godotenv.Load(".env")`（失敗僅 Warn）→ 2. `config.Load`（失敗 Fatal）→
3. `mariadb.OpenPool` + `InitSchema`（失敗 Fatal）→ 4. 初始回補/每日更新（失敗 Fatal）→
5. Discord boot notice（失敗僅 Error，非致命）→ 6. `go server.Run`（背景 Echo）→
7. `tradingSvc.DailyCheck`（阻塞的上線交易 loop）。

上線 loop 於台灣時間**開盤時段 09:10–09:30** 抓 TWSE MIS 即時開盤價、以開盤價即時決策
（`Engine.ProcessOpenDecision`），水位線去重、09:29 起逾時 fallback；啟動時 catch-up 用 DB 歷史
`open_price` 同基準回放。即時報價來源為 `internal/client/twse.RealtimeClient`（MIS getStockInfo.jsp，
經 `RealtimeFetcher` port 注入）。

**資金模型：期初一次性本金、不再外部注資（lump-sum 封閉資金池）**。`monthly_contribution` 定版為 **0**，
回測與上線都只動用期初 `initial_cash`（$100,000）。`monthly_contribution > 0` 仍是支援的選項（每月第一個交易日
注資，排程單一事實來源 `backtest.ContributionDue`，與回測 `ContributionAmounts` 逐日一致，累計額存 BotState
`total_contributed`）；定版設 0 時整條注資路徑為 no-op，資金加權報酬退化為 CAGR。首次啟動（無水位線）從
common issuance 起 catch-up，其現金軌跡與帳本與回測全期完全一致。

## 不可破壞的不變量（CRITICAL）

改動前務必確認下列規則不被破壞，否則行為會錯且測試會紅：

- **交易引擎必須維持純記憶體、零 I/O。** `internal/service/trading` 不得 import DB、Discord、HTTP、
  `config` 以外的副作用來源。價格一律由呼叫端傳入的 `StockSeries` 提供；DB/CSV 載入留在外層。
  副作用透過 `Executor` 介面（上線寫 ledger / 發 Discord；回測用 `NoopExecutor`）。
- **no-borrow 現金約束。** 買進前夾取 `maxAffordable = floor(cash/price)`；任何時刻 `cash < 0`
  立即回報 invariant violated 並中止。
- **golden fingerprint 測試。** `internal/service/backtest/characterization_test.go` 的
  `TestCharacterization_LiveStrategyFingerprint` 釘住 live 策略的回測指紋。**此測試失敗 = 你改到了
  策略行為。** 若非刻意調整策略，請回退；若刻意調整，需以 IS/OOS 驗證並更新指紋（且說明理由）。
- **決策成交價基準。** `decision_price_basis: open`（config.yaml）→ 當日**開盤價**成交、指標只看到
  **前一交易日收盤**（無未來資訊）；線上經 TWSE MIS 取即時開盤、回測/CSV 用歷史 `open_price`，兩邊同基準。
  帳本成交價由引擎決策價寫入（`PortfolioService.BuyShares/SellShares` 收 `price` 參數），**不可**改回
  `GetPriceAsOf` 查 DB（開盤決策當下 DB 尚無當日 K 棒，會誤拿 T-1 收盤）。現行參數為**開盤基準 + lump-sum 專調**
  （`regime_ma_window 85`、`trail_stop_bear 0.08`、`bull_buy_band 0.08`、`cooldown_break_budget 3`，
  00631L override `regime_ma_window 60` + `trail_reentry_cooldown_days 42`）；改回收盤基準、改決策基準、
  或重新開啟外部注資（`monthly_contribution > 0`）都需重新以 `cmd/eval_csv` 跑 walk-forward / IS-OOS 調參並重釘指紋。
- **API wire keys 不可變。** `internal/dto` 的 JSON tag 是前端既有契約（含唯一的 camelCase
  `todayClosePrice`），不得更名。新增端點 `/api/get_performance_summary`（`dto/performance.go`）的欄位為
  增量擴充；其比率欄位用 `dto.JSONFloat`（NaN/±Inf → `null`），不要改回裸 `float64`（`encoding/json` 無法編 NaN/Inf 會回錯）。
  後續再增量擴充（皆向後相容）：summary 加 `holding_ratio`/`cash_ratio`（資產配置比例）與 backtest 區塊的
  `equity_curve`（全期等距取樣最多 400 點的策略 vs B&H 權益曲線）；新增端點 `/api/get_equity_history`
  （`dto/equity.go` 的 `LiveEquityPoint`）回傳實盤每日權益時間序列供歷史折線圖。
- **realized-P&L 端點的日期格式。** DSN 不得加 `parseTime`；`RealizedGainsLosses` 的 `DATE` 欄位
  需維持既有 wire 格式（見近期 commit 402cd71）。
- **策略單一來源。** 目前只有 `Baseline` 現金比例策略；舊的固定金額金字塔已整組移除。
  所有可調參數集中在 `config.yaml`，不要在程式碼硬編策略數字。
- **per-stock override 採用準則。** `config.yaml` 的 `stock_overrides` 僅採用通過 IS/OOS 樣本內外
  驗證、樣本外不退化的覆寫（現行 `00631L: regime_ma_window 60` + `trail_reentry_cooldown_days 42`；後者僅套 2x 槓桿股，
  全股套用會過擬合）。
- **引擎記憶體狀態未持久化。** `peakSinceHold`、`lastTrailSell`（移動停利再進場冷卻用）等為純記憶體狀態，
  不寫入 DB；上線重啟靠 catch-up 回放重建，故 `init_db_back_months` 須涵蓋足夠回看（≥ 冷卻天數）才能正確還原。
- **BotState 持久化欄位。** 跨重啟持久化於 `BotState` 的鍵為 `last_processed_date`（水位線）、`current_cash`、
  `total_contributed`（累計外部注資，供 API 本金明細；`monthly_contribution=0` 定版下恆為 0）。`total_contributed`
  只在「有新注資的月份」累加；要與回測完全對齊請清空 BotState + 帳本讓其從 common issuance 重新 catch-up。
- **EquityHistory 每日權益快照。** 線上引擎 catch-up 回放與每日 loop 都會以 `date` 為 PK upsert（`RecordEquity`）
  一筆當日權益（現金 + 持股市值）到 `EquityHistory` 表，供 `/api/get_equity_history` 歷史折線圖。寫入失敗僅警告、不致命，
  不影響交易；與 `total_contributed` 同理只逐日累積（既有部署升級後不回填過往日期，清空 BotState + 帳本才會從 common issuance 補齊全期）。

## 程式碼與註解規範

詳見 [docs/development.md](docs/development.md)。要點：

- **gofmt/goimports 為強制**；多個小檔優於少數大檔（200–400 行常見，800 上限）。
- **註解政策（本專案特別要求）：**
  - 每個檔案開頭（line 1）以 `// <relative/path/file.go> <一句話責任>` 說明該檔功能。
  - 每個 `func`/`type`/重要 `const`/`var` 前有 doc comment，以識別字開頭（Go 慣例）。
  - 函式內每個「以空白行分隔的程式 block」前，加一行說明「這段在做什麼」。
  - 註解描述**現在的行為/功能**，**不寫**「為什麼這樣寫」「修 bug」「暫時」「TODO」等脈絡。
  - 語言為**繁體中文**；風格正式但淺顯。
  - 範例與密度標準：以 [internal/service/trading/engine.go](internal/service/trading/engine.go) 為準。
  - 測試檔（`_test.go`）只要求檔案標頭 + 函式 doc comment，不要求逐 block 註解。
- 錯誤一律 `fmt.Errorf("...: %w", err)` 包裝上下文；不可靜默吞錯。
- 介面小而專一，定義在使用端；accept interfaces, return structs。

## 測試慣例

- 標準 `go test` + table-driven；race 檢查 `go test -race ./...`。
- DB 測試用 `DATA-DOG/go-sqlmock`（assert 精確 SQL 字串 → **不要改動 repository 的 SQL 文字**）。
- TWSE client 測試用 `httptest`（`twseBaseURL` 可注入）。
- 回測/策略行為由 golden fingerprint 把關（見上）。
- 目標覆蓋率 80%+。

## 設定與機密

- `config.yaml`：非機密策略與回補參數，可 commit。
- `.env`：機密（`DB_DSN`、`DISCORD_BOT_TOKEN`、`DISCORD_BOT_CHANNELID`），不可 commit；範本見 `.env.example`。
- 不得硬編任何密鑰；啟動時驗證必要設定存在。

## 主要相依套件

Echo v4（HTTP）、go-sql-driver/mysql（DB driver）、bwmarrin/discordgo（通知）、
prometheus/client_golang（metrics）、sirupsen/logrus（log）、joho/godotenv（.env）、
gopkg.in/yaml.v3（config）、DATA-DOG/go-sqlmock（測試）。Module 宣告 `go 1.21.4`；CI 用 Go 1.22。

## 文件地圖

| 文件 | 內容 |
| --- | --- |
| [README.md](README.md) | 精簡入口與快速啟動 |
| [docs/overview.md](docs/overview.md) | 架構、資料流、套件責任 |
| [docs/development.md](docs/development.md) | 本機開發、命令、註解規範、測試 |
| [docs/api.md](docs/api.md) | REST 與維運端點 |
| [docs/strategy.md](docs/strategy.md) | 交易演算法、參數語意、資金安全規則 |
| [docs/backtest.md](docs/backtest.md) | 回測方法、重現指令、績效結果 |
| [docs/database-schema.md](docs/database-schema.md) | MariaDB schema 與寫入路徑 |
| [docs/deployment.md](docs/deployment.md) | Docker Compose、本機/正式機部署 |
| [docs/cicd-k8s.md](docs/cicd-k8s.md) | GitHub Actions 與 Kubernetes Secret |
| [docs/optimization/BEST-STRATEGY.md](docs/optimization/BEST-STRATEGY.md) | 策略最佳化研究紀錄 |

## 環境注意事項

- 開發機為 Windows（PowerShell）；repo 檔案使用 LF，git 可能提示 LF→CRLF 警告，屬正常。
- 本機 Go 版本可較新（如 1.24），與 `go.mod` 宣告的 1.21.4 相容。
