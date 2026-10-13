# API 文件

本專案使用 Echo 提供 HTTP API。正式 server 預設監聽 `:8080`，HTTPS 由 Caddy 或外部 Ingress 處理。

## 維運端點

| Method | Path | 說明 |
| --- | --- | --- |
| `GET` | `/health` | liveness probe，服務程序可回應即回 `{"status":"ok"}` |
| `GET` | `/ready` | readiness probe，會確認 DB 可 ping |
| `GET` | `/metrics` | Prometheus metrics |

## 業務端點

| Method | Path | 說明 |
| --- | --- | --- |
| `GET` | `/` | 簡易首頁訊息 |
| `GET` | `/api/get_unrealized_gains_losses` | 取得目前未實現持倉與損益 |
| `GET` | `/api/get_realized_gains_losses` | 取得已實現損益紀錄 |
| `GET` | `/api/get_stock_statistic_data` | 取得追蹤標的統計資料 |
| `GET` | `/api/get_stock_history_data?stock_id=00631L` | 取得指定股票歷史收盤資料 |
| `GET` | `/api/get_performance_summary` | 取得策略績效摘要（本金明細 + 實盤現況 + 回測指標 + 回測權益曲線） |
| `GET` | `/api/get_equity_history` | 取得實盤每日權益歷史（真實帳戶總權益時間序列，供歷史權益折線圖） |
| `GET` | `/api/get_performance_history` | 取得統一日期軸的逐日績效序列（回測 + 實盤兩組指標對齊同一時間軸） |
| `GET` | `/api/get_strategy_monitor?date=YYYY-MM-DD` | 取得指定日決策圖表；省略日期時取得最新保存紀錄 |
| `GET` | `/api/get_strategy_monitor_history` | 取得全部決策日期的精簡索引、來源與自動回補進度 |

## `/api/get_strategy_monitor`

此唯讀端點回傳最新或指定日期的已保存決策快照，供前端的「策略監控」頁呈現買入與賣出兩個區塊。
讀取 API 不會重新執行策略、下單、更新冷卻天數或改動帳本；頁面顯示的是 `decision_date` 當日的決策狀態，並非即時報價重新推算的訊號。

- **快照資訊**：`schema_version`、`availability`、`message`、`decision_date`、`recorded_at`、`decision_price_basis`、`execution_mode`、`indicator_rule`。
- **來源與版本**：`source` 為 `original` 或 `reconstructed`；`strategy_version`、`config_hash`、`strategy_config` 與 `unit_book` 凍結保存當次規則和價格／股數單位依據；`warnings` 說明回補限制。舊原始快照無版本證據時使用 `legacy-unknown`，不套用現在的設定。
- **各標的**：`stocks` 包含 `stock_id`、`status`、`decision_price`、`indicator_date`、市況判定與 `buy`／`sell`。
- **買入／賣出階段**：`status`、`reasons`、`conditions`、補充 `metrics`、預計與實際成交股數，以及該階段執行前後的現金及持股。
- **條件**：`id`、`label`、`status`、`value`、`threshold`、`operator`、`unit`、`description`；冷卻條件另含 `remaining_days` 與 `release_date`。

列舉值使用下列契約；前端應依各層級解讀，不能把所有 `status` 當作相同集合。

| 欄位 | 合法值與意義 |
| --- | --- |
| `availability` | `available`：有可查看圖表；`unavailable`：尚無快照，或該歷史日有紀錄但資料不足以可靠重建 |
| `stocks[].status` | `evaluated`：已評估；`inactive`：當日不適用，例如暫停交易；`unavailable`：決策資料不足 |
| `buy.status`／`sell.status` | `executed`：已成交；`eligible`：條件通過但沒有成交；`blocked`：條件未通過；`inactive`：未啟用或不適用；`unavailable`：資料不足 |
| `conditions[].status` | `passed`：通過；`blocked`：未通過；`inactive`：未啟用或不適用；`unavailable`：資料不足 |
| `conditions[].operator` | `lt`：小於；`gt`：大於；`gte`：大於等於；`eq`：等於 |
| `conditions[].unit`／`metrics[].unit` | `price`：股價；`currency`：金額；`percent`：百分比；`days`：日曆天；`shares`：股數；`ratio`：倍率；`regime`：市況數值（0 空頭、1 多頭） |

百分比欄位採百分點表示（0～100 尺度）：例如 `value: 3` 代表 3%，`threshold: 10` 代表 10%，前端不應再乘以 100。
無適用或無法取得的數值為 `null`。冷卻天數與解除日期以快照內的決策時點為準，不能依瀏覽器目前日期自行扣減。
開盤執行模式的技術指標只使用截至前一交易日的已完成價格資料；請搭配 `indicator_date`、`decision_price_basis` 與 `indicator_rule` 顯示資料時點。

尚未保存決策快照時，API 以 HTTP 200 回傳 `availability: "unavailable"` 及說明訊息，不可當成沒有策略觸發。指定日期若已保存資料不足的紀錄，同樣回傳 200，並保留日期與 `warnings`。
儲存讀取失敗或未注入監控服務時，回傳 HTTP 503 與 `{"error":"strategy_monitor_unavailable"}`，不以空快照假裝查詢成功。
`date` 必須是有效的 `YYYY-MM-DD`，空值或格式錯誤回傳 400 `invalid_decision_date`；不存在的日期回傳 404 `strategy_history_not_found`；指定日期讀取失敗為 503 `strategy_history_unavailable`。

每次成功決策會在帳本、現金、水位線同一資料庫交易內新增 `StrategyMonitorHistory`，包含無成交日。歷史以日期為鍵永久保留，不覆寫已保存的原始或重建紀錄；`BotState.strategy_monitor_v1` 保留最新原始快照供相容。舊部署尚存的最後原始快照會先封存，條件數值保持不變。

### `/api/get_strategy_monitor_history`

回傳 `entries`（日期降冪，只有摘要，不包含條件本文）、`oldest_date`、`newest_date` 與 `backfill`。每筆摘要包含 `decision_date`、`source`、`execution_mode`、`recorded_at`、`strategy_version`、`availability`。全部日期一次提供，供日期選擇與前後日切換；選取日期才載入完整圖表。索引直接查詢資料庫，新增原始決策立即可見。

`backfill` 含 `status`（`pending`、`running`、`complete`、`failed`）、`completed`、`total` 與 `message`。啟動後自動在背景補缺，成功後每五分鐘檢查新缺口，失敗三十秒後自動重試，既有紀錄仍可查看。

回補範圍僅限機器人有交易帳本或每日權益紀錄且不超過已提交水位線的日期。以可重複讀取的唯讀交易取得一致輸入後，釋放交易再計算及保存；不呼叫真實交易 executor、不通知、不修改帳本、現金、風險狀態或水位線。日期有確定資料缺損時保留可查閱的 `unavailable` 紀錄；最新尚未結算交易日則等待權益資料後重試。

`reconstructed` 的成交及持倉來自完整帳本，條件採回補時的策略設定與歷史行情。買入前、實際買入後的賣出前條件分別觀察；歷史多段賣出與未售股數會聚合還原原始買單。持倉峰值從最近一次全數出場後的連續持倉期間重建；有帳本成交日優先使用成交價，無成交日才用歷史行情。歷史策略版本、無成交日即時報價、股票處理順序及舊賣出原因不足以完整還原，因此圖表會明確列出假設，不宣稱為當時原始決策理由。既有紀錄即使設定或行情後續改變也保持不變。

## `/api/get_performance_summary`

一次回傳三類資訊，供前端呈現「投入了多少本金、目前賺賠多少、策略本身好不好」：

- **本金明細**（外部注入股市的資金，不含後續滾出的獲利）：
  `initial_cash`（期初一次性本金）、`monthly_contribution`（每月定額注資設定；定版為 0 = 關閉外部注資）、
  `total_contributed`（累計已注資；注資關閉時為 0）、`total_invested`（投入本金合計 = 期初 + 累計注資）。
- **實盤現況**（真實帳本 + BotState）：`current_cash`（未投入股市的預備現金）、`holding_value`（持股市值）、
  `total_equity`、`holding_ratio`（持股佔總資產比例 %）、`cash_ratio`（預備現金佔總資產比例 %；與 `holding_ratio` 合計約 100）、
  `realized_pnl`、`unrealized_pnl`、`total_pnl`（= 總權益 − 投入本金）、`total_return_rate`（%）。
- **回測績效** `backtest`（資料不足或評估失敗時為 `null`）：
  - 全期 headline：`span_start`/`span_end`/`years`/`total_in`，以及 `strategy` 與 `buy_hold` 各自的
    `final_equity`/`multiple`/`mwr`（資金加權年化報酬）/`max_drawdown`（NAV 回撤）/`calmar`/`sortino`/`avg_exposure`，
    外加策略交易統計 `buys`/`sells`/`trail_sells`/`profit_sells`（獲利了結機制已移除，恆為 0，key 保留向後相容）/`skipped`/`final_cash`。
  - `equity_curve` 全期（等距取樣，最多 400 點）每日權益曲線陣列，每點含 `date`/`strat_equity`/`bh_equity`，
    供前端繪製「策略 vs Buy & Hold」歷史權益折線圖。
  - `walk_forward` 多視窗穩健性 scorecard：中位 MWR / 回撤 / Calmar、`calmar_win_rate`、`blend_skill_rate`、
    `ret_participation`，及五道關卡 `g1_return_participation`～`g5_robustness` 與 `overall_pass`。

回測比率欄位（`mwr`/`calmar`/`sortino` 等）在邊界情況可能為 `null`（對應 NaN/±Inf，見 `dto.JSONFloat`）。
本金明細對齊回測與上線共用的 `monthly_contribution` 設定（定版為 0 = lump-sum），故實盤帳本與回測情境一致。

## `/api/get_equity_history`

回傳實盤真實帳戶的**每日權益時間序列**（升冪，等距取樣最多 400 點），供前端繪製歷史權益折線圖。
每筆含 `date`（`YYYY-MM-DD`）、`cash`（當日預備現金）、`holding_value`（當日持股市值）、`total_equity`（當日總權益）。

資料來源為線上引擎逐日寫入的 `EquityHistory` 表（catch-up 回放 + 每日 loop，以 `date` 為主鍵 upsert）。
與 `backtest.equity_curve` 不同，此為**真實帳本走勢**：

- 全新部署（清空 BotState + 帳本）首次啟動會從 common issuance 回放補齊全期曲線。
- 既有部署升級後從升級點起逐日累積（過往未持久化的日期不回填，與 `total_contributed` 的「只累計新月份」一致）。
- 無資料時回傳 `[]`。

## `/api/get_performance_history`

回傳**一條統一日期時間軸**的逐日序列（升冪、等距取樣 ≤ 400 點），每個日期點同時帶「回測」與「實盤」兩組指標，
供前端把模擬與真實表現畫在同一張折線圖。日期軸取自全期回測曲線（涵蓋共同上市日 ~ 今天）。

- **回測欄位**（全期皆有值）：`strat_equity`、`bh_equity`、`strat_multiple`、`bh_multiple`、
  `strat_return_rate`、`bh_return_rate`、`strat_drawdown`、`bh_drawdown`、`strat_cagr`（基期過短時為 `null`）。
- **實盤欄位**（go-live 後才有，之前為 `null`）：`cash`、`holding_value`、`total_equity`、`holding_ratio`、`cash_ratio`、
  `total_pnl`、`total_return_rate`、`multiple`、`realized_pnl`、`unrealized_pnl`、`cagr`、`max_drawdown`。
- **共用**：`invested`（投入本金到當日；lump-sum 為常數 = 期初本金）。

實盤欄位以指標型承載：對應日期無實盤快照（go-live 前）→ `null`。`unrealized_pnl = holding_value − cost_basis`、
`realized_pnl = (total_equity − invested) − unrealized_pnl`（由 `EquityHistory.cost_basis` 還原，不需另存已/未實現損益）。
單位：金額為元、倍數為倍、比率（report_rate / cagr / ratio / drawdown）為百分比。

回測 `*_drawdown` 與實盤 `max_drawdown` 為「距當下為止歷史高點」的累進回撤（非全期單一最大回撤）。

## 回應資料

API 回應由 `internal/dto` 定義。`portfolio.go` 對應投資組合損益，`market.go` 對應價格統計與歷史價格點，
`performance.go` 對應策略績效摘要（含 `JSONFloat`：NaN/±Inf 序列化為 `null`；以及回測 `equity_curve` 的 `EquityPoint`），
`equity.go` 對應實盤每日權益歷史（`LiveEquityPoint`），
`performance_history.go` 對應統一日期軸績效歷史（`PerformanceHistoryPoint`；實盤欄位為 `*float64`，nil → JSON `null`）。
`strategy_monitor.go` 對應實際交易決策監控快照（`StrategyMonitor`）。

## 錯誤處理

目前 controller 對部分查詢錯誤會回傳空陣列，維持前端既有行為。若未來要改成標準錯誤格式，需同步調整前端與測試。
`/api/get_strategy_monitor` 明確區分尚無快照（200 + `availability: "unavailable"`）與查詢失敗（503 + `strategy_monitor_unavailable`）。
