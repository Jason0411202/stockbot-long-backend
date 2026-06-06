package main

// cmd/fetch_data 是一個獨立的歷史資料抓取器：
//   - 直接打 TWSE STOCK_DAY API，逐月抓取追蹤標的的 OHLCV，
//   - 寫成本機 CSV 快取 (data/<stockID>.csv)，欄位: date,open,high,low,close,volume。
//
// 目的：讓 walk-forward 回測 / 參數掃描完全脫離 MariaDB 與 docker —— 只要有 CSV 就能跑。
// 與正式上線的 sqls.UpdataDatebase 互不影響 (那條路徑仍寫 DB)。
import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"main/helper"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type bar struct {
	date                   string // AD "2024-03-01"
	open, high, low, close float64
	volume                 float64
}

func main() {
	months := flag.Int("months", 90, "往前抓幾個月")
	outDir := flag.String("out", "data", "CSV 輸出目錄")
	stocksCSV := flag.String("stocks", "00631L,00830", "追蹤標的 (逗號分隔)")
	sleepMs := flag.Int("sleep", 1200, "每次 API 呼叫間隔 (毫秒)")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir 失敗:", err)
		os.Exit(1)
	}

	var stocks []string
	for _, s := range strings.Split(*stocksCSV, ",") {
		if s = strings.TrimSpace(s); s != "" {
			stocks = append(stocks, s)
		}
	}

	now := time.Now()
	// 產生 month anchors: 從本月往前 months 個月，每月取 1 號。
	anchors := make([]string, 0, *months+1)
	for i := 0; i <= *months; i++ {
		t := now.AddDate(0, -i, 0)
		anchors = append(anchors, fmt.Sprintf("%04d%02d01", t.Year(), int(t.Month())))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, stockID := range stocks {
		bars := make(map[string]bar, 2048)
		ok, fail := 0, 0
		for _, anchor := range anchors {
			rows, err := fetchMonth(client, anchor, stockID)
			if err != nil {
				fail++
				fmt.Fprintf(os.Stderr, "[warn] %s %s: %v\n", stockID, anchor, err)
				time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
				continue
			}
			for _, b := range rows {
				bars[b.date] = b
			}
			ok++
			fmt.Printf("  %s %s: +%d 列 (累計 %d 交易日)\n", stockID, anchor, len(rows), len(bars))
			time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
		}
		if len(bars) == 0 {
			fmt.Fprintf(os.Stderr, "[warn] %s 沒有抓到任何資料，跳過寫檔\n", stockID)
			continue
		}
		path := filepath.Join(*outDir, stockID+".csv")
		if err := writeCSV(path, bars); err != nil {
			fmt.Fprintf(os.Stderr, "[error] 寫 %s 失敗: %v\n", path, err)
			continue
		}
		fmt.Printf("✓ %s -> %s (%d 交易日, 月成功 %d / 失敗 %d)\n", stockID, path, len(bars), ok, fail)
	}
}

// fetchMonth 打 TWSE STOCK_DAY API 抓單月資料，回傳該月每日 OHLCV。
func fetchMonth(client *http.Client, dateYYYYMMDD, stockID string) ([]bar, error) {
	url := fmt.Sprintf("https://www.twse.com.tw/exchangeReport/STOCK_DAY?response=json&date=%s&stockNo=%s", dateYYYYMMDD, stockID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseStockDay(body)
}

// parseStockDay 解析 TWSE STOCK_DAY API 的 JSON body,回傳該月每日 OHLCV。
// 從 fetchMonth 抽出的純函式 (不依賴網路),方便單元測試各種輸入分支。
func parseStockDay(body []byte) ([]bar, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if stat, _ := payload["stat"].(string); stat != "OK" {
		return nil, fmt.Errorf("stat=%v (可能該月尚未上市或無資料)", payload["stat"])
	}
	rawData, ok := payload["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("無 data 欄位")
	}

	out := make([]bar, 0, len(rawData))
	for _, r := range rawData {
		row, ok := r.([]interface{})
		if !ok || len(row) < 7 {
			continue
		}
		cells := make([]string, len(row))
		for i, c := range row {
			s, _ := c.(string)
			cells[i] = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
		}
		adDate, err := helper.ROCToAD(cells[0])
		if err != nil {
			continue
		}
		o := parseF(cells[3])
		h := parseF(cells[4])
		l := parseF(cells[5])
		c := parseF(cells[6])
		v := parseF(cells[1])
		if c <= 0 { // 收盤價無效 (停牌 "--" 等) 直接跳過
			continue
		}
		out = append(out, bar{date: adDate, open: o, high: h, low: l, close: c, volume: v})
	}
	return out, nil
}

func parseF(s string) float64 {
	s = strings.ReplaceAll(s, "X", "")
	if s == "" || s == "--" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func writeCSV(path string, bars map[string]bar) error {
	dates := make([]string, 0, len(bars))
	for d := range bars {
		dates = append(dates, d)
	}
	sort.Strings(dates) // ISO 日期字串排序 = 時間排序 (升冪)

	var sb strings.Builder
	sb.WriteString("date,open,high,low,close,volume\n")
	for _, d := range dates {
		b := bars[d]
		sb.WriteString(fmt.Sprintf("%s,%.4f,%.4f,%.4f,%.4f,%.0f\n", b.date, b.open, b.high, b.low, b.close, b.volume))
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
