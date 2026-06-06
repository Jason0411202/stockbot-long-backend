package kernals

import (
	"bufio"
	"fmt"
	"main/config"
	"math"
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
// 與 loadStockSeries (DB 路徑) 等價地建立 *stockSeries,額外填入 OHLCV 供指標旗標使用。

// LoadSeriesFromCSV 從 dir 下的 <stockID>.csv 讀入所有 stocks 的歷史資料,建立 series map。
// 任一檔缺檔即回錯;單列解析失敗則略過該列。
func LoadSeriesFromCSV(dir string, stocks []string) (map[string]*stockSeries, error) {
	series := make(map[string]*stockSeries, len(stocks))
	for _, stockID := range stocks {
		path := filepath.Join(dir, stockID+".csv")
		s, err := loadOneCSV(path)
		if err != nil {
			return nil, fmt.Errorf("載入 %s: %w", path, err)
		}
		if len(s.dates) == 0 {
			return nil, fmt.Errorf("%s 無有效資料列", path)
		}
		series[stockID] = s
	}
	return series, nil
}

func loadOneCSV(path string) (*stockSeries, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		dates                     []time.Time
		closes, highs, lows, vols []float64
	)
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
	applySplitAdjust(closes, highs, lows)

	idx := make(map[string]int, len(dates))
	for i, d := range dates {
		idx[d.Format("2006-01-02")] = i
	}
	return &stockSeries{
		dates:       dates,
		dateIndex:   idx,
		closePrices: closes,
		highs:       highs,
		lows:        lows,
		volumes:     vols,
		ma20:        rollingMA(closes, 20),
		prefixClose: buildPrefixClose(closes),
	}, nil
}

// rollingMA 回傳 window 日簡單移動平均;不足 window 日的位置為 NaN。
func rollingMA(prices []float64, window int) []float64 {
	out := make([]float64, len(prices))
	sum := 0.0
	for i, p := range prices {
		sum += p
		if i >= window {
			sum -= prices[i-window]
		}
		if i >= window-1 {
			out[i] = sum / float64(window)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func reorderF(src []float64, idxs []int) []float64 {
	out := make([]float64, len(src))
	for i, j := range idxs {
		out[i] = src[j]
	}
	return out
}

func reorderTime(src []time.Time, idxs []int) []time.Time {
	out := make([]time.Time, len(src))
	for i, j := range idxs {
		out[i] = src[j]
	}
	return out
}

// EvaluateWalkForward 是 walkForwardOnSeries 的匯出包裝,供 cmd 對給定 cfg 取得 scorecard。
// 回傳跨視窗彙整 (AggregateReport) 與每視窗明細。
func EvaluateWalkForward(cfg *config.Config, series map[string]*stockSeries, p WalkForwardParams) ([]WindowReport, AggregateReport, error) {
	return walkForwardOnSeries(cfg, series, p)
}
