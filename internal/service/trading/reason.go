// internal/service/trading/reason.go 定義交易決策理由的結構化載體與人類可讀摘要。
package trading

import "fmt"

// reason.go 收錄「為什麼這筆會成交」的純資料結構 TradeReason 與其繁體中文摘要產生器。
// 它由決策函式 (DecideBuy / DecideSell) 填入決策端欄位、由引擎 apply 時補上成交端欄位 (股數 / 金額 / 餘額),
// 最後供上線執行器寫入 log 與組裝 Discord 通知。純資料 + 純格式化,無任何 I/O。

// TradeReason 描述一筆成交的決策理由 (供 log 結構化欄位與 Discord 通知排版使用)。
type TradeReason struct {
	Action  string // "buy" / "sell"
	Trigger string // "dip"(逢低買入) / "trail"(熊市移動停利) / "profit"(多頭獲利了結)
	Regime  string // "bull"(牛市) / "bear"(熊市)

	// 成交端 (引擎 apply 後補上)。
	Price     float64 // 成交價 (open 基準 = 當日開盤價)
	Shares    int     // 實際成交股數 (已過現金夾取)
	Amount    float64 // 成交金額 = Price × Shares
	CashAfter float64 // 成交後剩餘現金

	// 買入決策端。
	EntryMA       float64 // 進場均線 (當日決策可見到的最後一筆)
	BandPct       float64 // 牛市放寬帶寬 (熊市為 0)
	DepthPct      float64 // 深度基準值 (peak 基準 = 距近期高點回撤;越負跌越深)
	BrokeCooldown bool    // 是否動用「打破冷卻」額度提前買入

	// 賣出決策端。
	GainPct      float64 // profit:相對最低成本獲利;trail:自峰值回落前的峰值相對成本獲利
	TrailStopPct float64 // trail:移動停利回撤門檻
}

// regimeLabel 回傳 regime 的繁中標籤 (牛市 / 熊市)。
func (r TradeReason) regimeLabel() string {
	if r.Regime == "bull" {
		return "牛市"
	}
	return "熊市"
}

// Summary 回傳一句話的繁體中文交易理由,供 log 與 Discord 通知共用。
func (r TradeReason) Summary() string {
	switch r.Trigger {
	case "dip":
		// 逢低買入:牛市放寬帶寬、熊市嚴格 <均線;附距高點回撤與是否打破冷卻。
		band := ""
		if r.Regime == "bull" && r.BandPct > 0 {
			band = fmt.Sprintf("×(1+%.0f%%)", r.BandPct*100)
		}
		s := fmt.Sprintf("%s逢低買入:開盤價 %.2f 低於進場均線 %.2f%s;距近期高點回撤 %.1f%%",
			r.regimeLabel(), r.Price, r.EntryMA, band, -r.DepthPct*100)
		if r.BrokeCooldown {
			s += ";已動用「打破冷卻」額度提前進場"
		}
		return s
	case "trail":
		// 熊市移動停利:自持有期間峰值回落達門檻,全數出場保護獲利。
		return fmt.Sprintf("熊市移動停利:自持有期間峰值回落達 %.0f%%,以開盤價 %.2f 全數出場鎖住獲利",
			r.TrailStopPct*100, r.Price)
	case "profit":
		// 多頭獲利了結:相對最低成本獲利達門檻,分批賣出。
		return fmt.Sprintf("多頭獲利了結:相對最低成本獲利已達 +%.0f%%,以開盤價 %.2f 分批賣出 %d 股",
			r.GainPct*100, r.Price, r.Shares)
	}
	return ""
}
