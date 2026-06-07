// internal/service/backtest/datacache.go 從本機 CSV 快取載入回測用 StockSeries。
package backtest

import (
	"bufio"
	"fmt"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/service/trading"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// datacache.go 提供「從本機 CSV 快取載入歷史資料」的離線路徑,讓 walk-forward 回測 / 參數掃描
// 完全不依賴 MariaDB 與 docker。CSV 由 cmd/fetch_data 產生,欄位: date,open,high,low,close,volume。
//
// 與 loadStockSeries (DB 路徑) 等價地建立 *trading.StockSeries,額外填入 OHLCV 供指標旗標使用。

// LoadSeriesFromCSV 從 dir 下的 <stockID>.csv 讀入所有 stocks 的歷史資料,建立 series map。
// 任一檔缺檔即回錯;單列解析失敗則略過該列。
func LoadSeriesFromCSV(dir string, stocks []string) (map[string]*trading.StockSeries, error) {
	series := make(map[string]*trading.StockSeries, len(stocks))
	for _, stockID := range stocks {
		path := filepath.Join(dir, stockID+".csv")
		s, err := loadOneCSV(path)
		if err != nil {
			return nil, fmt.Errorf("載入 %s: %w", path, err)
		}
		if len(s.Dates) == 0 {
			return nil, fmt.Errorf("%s 無有效資料列", path)
		}
		series[stockID] = s
	}
	return series, nil
}

// loadOneCSV 讀取單一股票 CSV 並建立已排序、已 split-adjust 的 StockSeries。
func loadOneCSV(path string) (*trading.StockSeries, error) {
	// 開啟 CSV 檔案,確保離開時關閉。
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		dates                     []time.Time
		closes, highs, lows, vols []float64
	)
	// 逐行掃描 CSV,略過標頭、空行與解析失敗的列;收盤價 <= 0 視為無效資料略過。
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if lineNo == 1 && strings.HasPrefix(line, "date,") {
			continue // header
		}
		cols := strings.Split(line, ",")
		if len(cols) < 5 {
			continue
		}
		t, err := time.Parse("2006-01-02", cols[0])
		if err != nil {
			continue
		}
		c := parseFloat(cols[4])
		if c <= 0 {
			continue
		}
		dates = append(dates, t)
		highs = append(highs, parseFloat(cols[2]))
		lows = append(lows, parseFloat(cols[3]))
		closes = append(closes, c)
		if len(cols) >= 6 {
			vols = append(vols, parseFloat(cols[5]))
		} else {
			vols = append(vols, 0)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// CSV 已由 fetcher 升冪寫入,但保險起見再排序一次 (以日期為鍵同步重排所有欄位)。
	if !sort.SliceIsSorted(dates, func(i, j int) bool { return dates[i].Before(dates[j]) }) {
		idxs := make([]int, len(dates))
		for i := range idxs {
			idxs[i] = i
		}
		sort.SliceStable(idxs, func(a, b int) bool { return dates[idxs[a]].Before(dates[idxs[b]]) })
		dates = reorderTime(dates, idxs)
		closes = reorderF(closes, idxs)
		highs = reorderF(highs, idxs)
		lows = reorderF(lows, idxs)
		vols = reorderF(vols, idxs)
	}

	// 還原股票分割 (split):使價格序列連續,再計算 MA / 前綴和 / peak。
	trading.ApplySplitAdjust(closes, highs, lows)

	return trading.NewStockSeries(dates, closes, highs, lows, vols), nil
}

// parseFloat 將 CSV 欄位轉成 float64，解析失敗時回傳 0。
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// reorderF 依排序後索引重排 float64 欄位。
func reorderF(src []float64, idxs []int) []float64 {
	out := make([]float64, len(src))
	for i, j := range idxs {
		out[i] = src[j]
	}
	return out
}

// reorderTime 依排序後索引重排日期欄位。
func reorderTime(src []time.Time, idxs []int) []time.Time {
	out := make([]time.Time, len(src))
	for i, j := range idxs {
		out[i] = src[j]
	}
	return out
}

// EvaluateWalkForward 是 walkForwardOnSeries 的匯出包裝,供 cmd 對給定 cfg 取得 scorecard。
// 回傳跨視窗彙整 (AggregateReport) 與每視窗明細。
func EvaluateWalkForward(cfg *config.Config, series map[string]*trading.StockSeries, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	return walkForwardOnSeries(cfg, series, p)
}
