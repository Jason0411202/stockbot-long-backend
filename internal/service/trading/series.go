// internal/service/trading/series.go 定義交易引擎使用的單檔股票時間序列。
package trading

import (
	"sort"
	"time"
)

// series.go 收錄純引擎共用的歷史資料載體 StockSeries 與其查詢方法。
// StockSeries 為單一股票經整理後的歷史資料,供引擎查價與均線。純記憶體結構,無任何 I/O 依賴。

// lot 為記憶體中的單筆未實現持倉。
type lot struct {
	date   time.Time
	shares int
	price  float64
}

// StockSeries 為單一股票經整理後的歷史資料,供引擎查價與均線。
//
// 欄位匯出以供跨套件 (backtest / 上線 shim) 建構;peakCache 維持未匯出 (lazy 內部快取)。
// 一般建構請走 NewStockSeries —— 它會以與 DB / CSV 路徑完全相同的方式計算 MA20 與 PrefixClose,
// 確保黃金指紋不變。
type StockSeries struct {
	Dates       []time.Time // asc
	DateIndex   map[string]int
	ClosePrices []float64
	MA20        []float64 // MA20[i] = 截至 Dates[i] 的 20 日均價;不足 20 日以 NaN 表示

	// 選用欄位:DB 路徑填 OpenPrices + PrefixClose;CSV 快取路徑另填 OHLCV。供旋鈕計算指標用。
	// OpenPrices 為「開盤價基準決策」(decision_price_basis=open) 的成交價來源;close 基準時不讀取。
	OpenPrices  []float64 // 開盤價 (可為 nil;close 基準路徑不需要)
	Highs       []float64 // 最高價 (可為 nil)
	Lows        []float64 // 最低價 (可為 nil)
	Volumes     []float64 // 成交量 (可為 nil)
	PrefixClose []float64 // 收盤價前綴和,供任意視窗 O(1) 均線查詢

	peakCache map[int][]float64 // lookback -> 近 lookback 日 (含當日) 最高收盤 (lazy)
}

// NewStockSeries 由升冪日期 + OHLCV 建立完整 StockSeries,並以「與 loadStockSeries / loadOneCSV
// 完全相同的方式」預先計算 MA20 = RollingMA(closes,20) 與 PrefixClose = BuildPrefixClose(closes)。
// opens/highs/lows/vols 可為 nil (close 基準的精簡路徑)。呼叫端須先完成 split-adjust 與排序。
func NewStockSeries(dates []time.Time, opens, closes, highs, lows, vols []float64) *StockSeries {
	// 建立日期字串 → 索引的反查表,供 ProcessDay 以 O(1) 定位當日位置。
	idx := make(map[string]int, len(dates))
	for i, d := range dates {
		idx[d.Format("2006-01-02")] = i
	}
	// 一併預算 MA20 與前綴和,使後續指標查詢皆為 O(1)。
	return &StockSeries{
		Dates:       dates,
		DateIndex:   idx,
		ClosePrices: closes,
		MA20:        RollingMA(closes, 20),
		OpenPrices:  opens,
		Highs:       highs,
		Lows:        lows,
		Volumes:     vols,
		PrefixClose: BuildPrefixClose(closes),
	}
}

// OpenAt 回傳 index i 的開盤價;OpenPrices 未填或越界時回退到該日收盤價 (close 基準語意下不受影響)。
// 供「開盤價基準決策」取成交價;退回 close 確保缺開盤資料時不致回傳 0 而誤判為無效價。
func (s *StockSeries) OpenAt(i int) float64 {
	if i < 0 || i >= len(s.ClosePrices) {
		return 0
	}
	if i < len(s.OpenPrices) && s.OpenPrices[i] > 0 {
		return s.OpenPrices[i]
	}
	return s.ClosePrices[i]
}

// CloseAsOf 回傳「在 day 當天或之前最近一個交易日」的收盤價 (as-of 查價)。
// 用於在「聯集日期」上替某檔沒交易的股票估值 (例如某檔放假、或尚未上市)。
//   - day 早於該股第一筆資料 (尚未上市) -> (0, false)。
//   - 其餘 -> 最近一個 <= day 的收盤價, true。
//
// 只看過去資料,絕無未來資訊洩漏;O(log n) 走既有已排序的 Dates。
// 注意:不可用 DateIndex (只含精確交易日),也不可用 ClosePrices[len-1] (那是全序列最後價)。
func (s *StockSeries) CloseAsOf(day time.Time) (float64, bool) {
	// 二分搜尋找出第一個嚴格晚於 day 的位置;i-1 即最近的 <= day 交易日。
	i := sort.Search(len(s.Dates), func(i int) bool { return s.Dates[i].After(day) })
	// i==0 代表 day 早於所有資料 (尚未上市),無法估值。
	if i == 0 {
		return 0, false
	}
	return s.ClosePrices[i-1], true
}

// CollectDateUnion 回傳所有股票日期的聯集,升冪排序。
func CollectDateUnion(series map[string]*StockSeries) []time.Time {
	// 以字串 set 去重;預估容量 2048 以減少 map 擴容。
	seen := make(map[string]struct{}, 2048)
	for _, s := range series {
		for _, d := range s.Dates {
			seen[d.Format("2006-01-02")] = struct{}{}
		}
	}
	// 將去重後的日期字串解析回 time.Time。
	out := make([]time.Time, 0, len(seen))
	for k := range seen {
		t, err := time.Parse("2006-01-02", k)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	// 升冪排序後回傳,供 ProcessDates 依序處理。
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
