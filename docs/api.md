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
    外加策略交易統計 `buys`/`sells`/`trail_sells`/`profit_sells`/`skipped`/`final_cash`。
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

## 錯誤處理

目前 controller 對部分查詢錯誤會回傳空陣列，維持前端既有行為。若未來要改成標準錯誤格式，需同步調整前端與測試。
