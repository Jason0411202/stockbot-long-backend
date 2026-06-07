// Package twse 提供單一、型別化的台灣證交所 (TWSE) STOCK_DAY 行情抓取客戶端。
//
// 它合併了既有兩條 TWSE 抓取路徑的行為:
//   - sqls.TWSEapi:去逗號 / 去 'X'、由 title 第三欄取股名、檢查 data key。
//   - cmd/fetch_data.fetchMonth + parseStockDay:30s timeout、Mozilla User-Agent、
//     檢查 stat=="OK"、解析型別化 OHLCV float、丟棄 close<=0 的無效列、升冪排列。
//
// 本套件僅依賴 stdlib + internal/entity + helper,不引入 app_context 或 logging,
// 所有錯誤都以回傳值往上拋 (errors out, no logging),方便上層自行決定如何記錄。
package twse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/helper"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// defaultBaseURL 為 TWSE STOCK_DAY API 端點 (正式預設指向真實端點)。
const defaultBaseURL = "https://www.twse.com.tw/exchangeReport/STOCK_DAY"

// defaultTimeout 沿用 cmd/fetch_data 的 30s 上限,避免單次抓取無限期阻塞。
const defaultTimeout = 30 * time.Second

// TWSE STOCK_DAY 每列欄位索引 (對齊 parseStockDay):
//
//	[0] date  [1] volume  [2] value  [3] open  [4] high  [5] low
//	[6] close [7] change  [8] transactions
const (
	colDate   = 0
	colVolume = 1
	colOpen   = 3
	colHigh   = 4
	colLow    = 5
	colClose  = 6
	// minColumns 為解析一列所需的最少欄位數 (需讀到 colClose=6)。
	minColumns = 7
	// titleStockNameIdx 為 title 以空白切分後股票名稱所在欄位
	// (例: "113年01月 00631L 元大台灣50正2 日成交資訊" → 第 3 欄為股名)。
	titleStockNameIdx = 2
)

// Client 為型別化的 TWSE 行情客戶端。零值不可用,請用 NewClient 建立。
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// Option 以函式選項模式設定 Client。
type Option func(*Client)

// WithBaseURL 覆寫 API 端點,主要供 httptest 在測試中注入假伺服器。
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

// WithHTTPClient 覆寫底層 http.Client,供注入自訂 timeout / transport 或測試替身。
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// NewClient 建立 TWSE 客戶端;預設端點為真實 TWSE,預設 http.Client timeout 為 30s。
func NewClient(opts ...Option) *Client {
	// 以預設 timeout 與正式端點初始化 Client。
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
	}
	// 依序套用呼叫端傳入的 Option 覆寫預設值。
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchMonth 抓取單月 STOCK_DAY 資料,回傳升冪 (舊→新) 的 OHLCV bars 與股票名稱。
//
// date 為 TWSE 要求的 "YYYYMMDD" (該月任一日皆回整月),stockID 為證券代號。
// 流程:GET 端點 → 要求 stat=="OK" 且存在 data key → 逐列去逗號 / 去 'X'、
// ROC 轉 AD 日期、解析型別化 float、丟棄無法解析或 close<=0 的列。
func (c *Client) FetchMonth(date, stockID string) (bars []entity.Bar, stockName string, err error) {
	// 組裝查詢 URL 並設定 Mozilla User-Agent 以符合 TWSE 要求。
	url := fmt.Sprintf("%s?response=json&date=%s&stockNo=%s", c.baseURL, date, stockID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("twse: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	// 發送 HTTP GET 請求並驗證 HTTP 狀態碼。
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("twse: http get %s: %w", stockID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("twse: unexpected status %d for %s", resp.StatusCode, stockID)
	}

	// 解碼 JSON 回應並驗證 stat 欄位與 data 欄位是否存在。
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, "", fmt.Errorf("twse: decode json: %w", err)
	}

	if stat, _ := payload["stat"].(string); stat != "OK" {
		return nil, "", fmt.Errorf("twse: stat=%v (該月可能尚未上市或無資料)", payload["stat"])
	}

	rawData, ok := payload["data"].([]interface{})
	if !ok {
		return nil, "", fmt.Errorf("twse: 回傳資料缺少 data 欄位")
	}

	// 解析列資料並從 title 欄位取得股票名稱。
	bars = parseRows(rawData)
	stockName = extractStockName(payload)
	return bars, stockName, nil
}

// parseRows 將 TWSE data 二維陣列轉成升冪 (舊→新) 的 entity.Bar 切片。
// 跳過欄位不足、日期無法轉換、或收盤價 <=0 的列 (mirror parseStockDay 的健壯性)。
func parseRows(rawData []interface{}) []entity.Bar {
	bars := make([]entity.Bar, 0, len(rawData))
	for _, r := range rawData {
		// 跳過型別不符或欄位數不足的列。
		row, ok := r.([]interface{})
		if !ok || len(row) < minColumns {
			continue
		}

		// 將每個欄位轉字串並清洗千分位逗號與 'X' 標記。
		cells := make([]string, len(row))
		for i, raw := range row {
			s, _ := raw.(string)
			cells[i] = cleanCell(s)
		}

		// 將民國年日期轉為西元年;轉換失敗則跳過該列。
		adDate, err := helper.ROCToAD(cells[colDate])
		if err != nil {
			continue
		}

		// 收盤價無效 (停牌 "--" 等) 直接跳過,避免寫入無意義資料。
		closePrice := parseFloat(cells[colClose])
		if closePrice <= 0 { // 收盤價無效 (停牌 "--" 等) 直接跳過
			continue
		}

		// 組裝 entity.Bar 並加入結果切片。
		bars = append(bars, entity.Bar{
			Date:   adDate,
			Open:   parseFloat(cells[colOpen]),
			High:   parseFloat(cells[colHigh]),
			Low:    parseFloat(cells[colLow]),
			Close:  closePrice,
			Volume: parseFloat(cells[colVolume]),
		})
	}
	return bars
}

// extractStockName 由 title (例 "113年01月 00631L 元大台灣50正2 日成交資訊") 取出股名,
// 並對欄位數做長度保護,缺少時回空字串而非 panic。
func extractStockName(payload map[string]interface{}) string {
	title, _ := payload["title"].(string)
	fields := strings.Fields(title)
	if len(fields) <= titleStockNameIdx {
		return ""
	}
	return fields[titleStockNameIdx]
}

// cleanCell 去除千分位逗號與 'X' 標記並修剪空白 (合併兩條舊路徑的清洗規則)。
func cleanCell(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "X", "")
	return s
}

// parseFloat 將已清洗的欄位轉 float;空值 / "--" / 無法解析皆回 0 (sentinel,由呼叫端判斷)。
func parseFloat(s string) float64 {
	if s == "" || s == "--" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
