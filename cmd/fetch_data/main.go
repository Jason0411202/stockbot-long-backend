package main

// cmd/fetch_data 是一個獨立的歷史資料抓取器：
//   - 透過 internal/client/twse 逐月抓取追蹤標的的 OHLCV，
//   - 寫成本機 CSV 快取 (data/<stockID>.csv)，欄位: date,open,high,low,close,volume。
//
// 目的：讓 walk-forward 回測 / 參數掃描完全脫離 MariaDB 與 docker —— 只要有 CSV 就能跑。
// TWSE 抓取邏輯已統一在 internal/client/twse (與上線寫 DB 路徑共用同一客戶端)。
import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/twse"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

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

	client := twse.NewClient()
	for _, stockID := range stocks {
		bars := make(map[string]entity.Bar, 2048)
		ok, fail := 0, 0
		for _, anchor := range anchors {
			rows, _, err := client.FetchMonth(anchor, stockID)
			if err != nil {
				fail++
				fmt.Fprintf(os.Stderr, "[warn] %s %s: %v\n", stockID, anchor, err)
				time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
				continue
			}
			for _, b := range rows {
				bars[b.Date] = b
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

// writeCSV 依日期升冪寫出含表頭的 CSV (date,open,high,low,close,volume)。
func writeCSV(path string, bars map[string]entity.Bar) error {
	dates := make([]string, 0, len(bars))
	for d := range bars {
		dates = append(dates, d)
	}
	sort.Strings(dates) // ISO 日期字串排序 = 時間排序 (升冪)

	var sb strings.Builder
	sb.WriteString("date,open,high,low,close,volume\n")
	for _, d := range dates {
		b := bars[d]
		sb.WriteString(fmt.Sprintf("%s,%.4f,%.4f,%.4f,%.4f,%.0f\n", b.Date, b.Open, b.High, b.Low, b.Close, b.Volume))
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
