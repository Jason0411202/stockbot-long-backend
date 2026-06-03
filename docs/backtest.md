# 績效

## 真實資料回測 (006208, 00830, 過去 60 個月)

以 `config.yaml` 預設設定 (`initial_cash=100000`, `buy_and_sell_multiplier=2.0`, `cooldown_days=14`, `init_db_back_months=60`, `back_testing_months=60`) 執行 `go run ./cmd/research_run`。

> 歷史紀錄：早期 config 用的是 `back_testing_days: 3600`，但因為 `init_db_back_months=60` 只提供 ~5 年資料，
> 實際被靜默截尾成 ~1231 天的回測結果。後續改成 `back_testing_months` 為單位後，跟 `init_db_back_months` 同單位、可精確比對，
> sanity check (見 `config/config_test.go`) 不再需要靠「每月平均交易日 × 22」做近似。
> 下方數值需要重跑才會更新；重跑指令：`go run ./cmd/research_run` (本機需要 DB 連線) 或 `docker compose exec app …`。

| 指標 | 值 (來自舊版 3600 days→1231 天靜默截尾的 run，僅供參考) |
| --- | --- |
| TrackStocks | 006208, 00830 |
| BackTestingMonths (current default) | 60 |
| InitialCash | 100,000.00 |
| FinalCash | 213,903.40 |
| FinalHoldingValue | 68,416.45 |
| FinalTotal | 282,319.85 |
| TotalBuys | 168 |
| TotalSells | 149 |
| SkippedBuys | 252 |
| PnL vs Initial | +182,319.85 (+182.32%) |

`SkippedBuys=252` 直接印證了「禁止借錢」夾取在真實資料上真的會被觸發 — 若沒有這條不變量，當前 initial_cash 下的 baseline 策略會在數百次買進時把現金夾到負數、產生隱性槓桿，讓歷史績效被高估。
