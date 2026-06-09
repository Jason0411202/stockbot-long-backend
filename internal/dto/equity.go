// internal/dto/equity.go 定義實盤每日權益歷史 API 的回應 DTO (供前端歷史權益折線圖)。
package dto

// LiveEquityPoint 是 GET /api/get_equity_history 的單筆回應:某交易日收盤後的真實帳戶權益快照。
// 資料來源為線上引擎逐日寫入的 EquityHistory 表 (現金 + 持股市值,皆四捨五入 2 位);
// 與回測 EquityCurve 不同,此為真實帳本走勢,隨上線運行逐日累積。
type LiveEquityPoint struct {
	Date         string  `json:"date"`          // 交易日 (YYYY-MM-DD)
	Cash         float64 `json:"cash"`          // 當日閒置現金 (未投入股市的預備現金)
	HoldingValue float64 `json:"holding_value"` // 當日持股市值 (以當日或之前最近收盤價估)
	TotalEquity  float64 `json:"total_equity"`  // 當日總權益 = 現金 + 持股市值
}
