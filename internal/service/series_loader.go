// internal/service/series_loader.go 將 repository 歷史資料轉為交易引擎使用的 StockSeries。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
)

// LoadTradingSeries 由 DB（透過 SeriesLoader port）載入每檔股票的歷史資料，並建構成
// 引擎可用的 trading.StockSeries map。它將「DB rows → trading.StockSeries」的建構邏輯
// 抽成可共用的匯出函式，讓 cmd/research_run、cmd/evaluate 與 cmd/server（透過 TradingService）
// 共用同一條路徑，確保行為完全一致。
//
// 對每檔 stockID：讀取（date, close_price）→ 以 "2006-01-02" 解析日期（失敗者跳過）→
// ApplySplitAdjust 還原分割 → NewStockSeries（highs/lows/vols 皆 nil，與 DB 路徑相同）。
// 沒有任何有效資料的股票會被略過（不出現在回傳 map 中）。
func LoadTradingSeries(ctx context.Context, loader SeriesLoader, stockIDs []string) (map[string]*trading.StockSeries, error) {
	raw, err := loader.LoadSeries(ctx, stockIDs)
	if err != nil {
		return nil, fmt.Errorf("load series: %w", err)
	}

	series := make(map[string]*trading.StockSeries, len(stockIDs))
	for _, stockID := range stockIDs {
		history := raw[stockID]

		// 解析日期字串並收集有效的時間點與收盤價序列。
		dates := make([]time.Time, 0, len(history))
		prices := make([]float64, 0, len(history))
		for _, h := range history {
			t, perr := time.Parse(dateLayout, h.Date)
			if perr != nil {
				t, perr = time.Parse(datetimeLayout, h.Date)
				if perr != nil {
					continue
				}
			}
			dates = append(dates, t)
			prices = append(prices, h.ClosePrice)
		}

		if len(dates) == 0 {
			continue
		}

		// 還原股票分割 (split):使價格序列連續,再由 NewStockSeries 計算 MA / 前綴和。
		trading.ApplySplitAdjust(prices)
		series[stockID] = trading.NewStockSeries(dates, prices, nil, nil, nil)
	}

	return series, nil
}
