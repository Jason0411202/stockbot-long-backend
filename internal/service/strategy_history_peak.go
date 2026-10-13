// internal/service/strategy_history_peak.go 依完整帳本還原連續持倉期間的歷史決策高點。
package service

import (
	"math"
	"sort"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// historicalPeakDay 彙整單日買入、賣出與可確認的最高決策價。
type historicalPeakDay struct {
	buys, sells int
	price       float64
}

// historicalPeaks 回傳 before 決策日之前的持倉高點；完整出清後重新起算。
// 同日先買入、更新價格再賣出，日期及價格均不讀取 before 當日或未來資料。
func historicalPeaks(lots []historicalLot, trades map[string][]trading.HistoricalTrade, series map[string]*trading.StockSeries, before time.Time, openBasis bool) map[string]float64 {
	// 按股票及日曆日建立事件，片段合計保留原始完整買單的股數。
	cutoff := before.Format(dateLayout)
	events := map[string]map[string]*historicalPeakDay{}
	dayEvent := func(id, date string) *historicalPeakDay {
		if date >= cutoff {
			return nil
		}
		if events[id] == nil {
			events[id] = map[string]*historicalPeakDay{}
		}
		if events[id][date] == nil {
			events[id][date] = &historicalPeakDay{}
		}
		return events[id][date]
	}
	for _, lot := range lots {
		if event := dayEvent(lot.stock, lot.buy.Format(dateLayout)); event != nil {
			event.buys += lot.shares
			event.addPrice(lot.price)
		}
		if !lot.sell.IsZero() {
			if event := dayEvent(lot.stock, lot.sell.Format(dateLayout)); event != nil {
				event.sells += lot.shares
			}
		}
	}

	// 成交日採帳本決策價；舊資料同日多個成交價時保存已知價格的最大值。
	for date, recorded := range trades {
		for _, trade := range recorded {
			if event := dayEvent(trade.StockID, date); event != nil {
				event.addPrice(trade.Price)
			}
		}
	}

	// 由完整成交流水找出仍持有部位的連續建倉日，無成交日不增加事件。
	peaks := make(map[string]float64, len(events))
	for id, daily := range events {
		dates := make([]string, 0, len(daily))
		for date := range daily {
			dates = append(dates, date)
		}
		sort.Strings(dates)
		held, start := 0, ""
		for _, date := range dates {
			event := daily[date]
			if held == 0 && event.buys > 0 {
				start = date
			}
			held += event.buys
			held -= event.sells
			if held <= 0 {
				start = ""
			}
		}
		peaks[id] = 0
		if start == "" {
			continue
		}

		// 目前這段連續持倉內的買入成本與真實成交價均參與高點。
		peak := &historicalPeakDay{}
		for _, date := range dates {
			if date >= start {
				peak.addPrice(daily[date].price)
			}
		}

		// 無成交日才採用行情；成交日價格已由帳本固定，不混入另一個報價。
		if s := series[id]; s != nil {
			first := sort.Search(len(s.Dates), func(i int) bool { return s.Dates[i].Format(dateLayout) >= start })
			for i := first; i < len(s.Dates) && s.Dates[i].Format(dateLayout) < cutoff; i++ {
				if recorded := daily[s.Dates[i].Format(dateLayout)]; recorded != nil && recorded.price > 0 {
					continue
				}
				if s.Suspended(s.Dates[i]) {
					continue
				}
				if openBasis {
					if i < len(s.OpenPrices) {
						peak.addPrice(s.OpenPrices[i])
					}
				} else if i < len(s.ClosePrices) {
					peak.addPrice(s.ClosePrices[i])
				}
			}
		}
		peaks[id] = peak.price
	}
	return peaks
}

// addPrice 納入有效的正價格，忽略行情缺值與非有限數字。
func (d *historicalPeakDay) addPrice(price float64) {
	if price > d.price && !math.IsNaN(price) && !math.IsInf(price, 0) {
		d.price = price
	}
}
