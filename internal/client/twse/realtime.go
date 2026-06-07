// internal/client/twse/realtime.go 提供 TWSE 基本市況報導 (MIS) 即時報價抓取,盤中取得當日開盤價。
package twse

// realtime.go 補上「盤中即時報價」路徑:STOCK_DAY 月資料盤後才公布,當日開盤無法用;
// 線上「開盤即時決策」改打 TWSE MIS (mis.twse.com.tw) 取當日開盤價 o (09:00 集合競價後即出現且全日固定)。
// MIS 每 5 秒更新一次、rate limit 約 3 req/5s,故所有追蹤股以單一 ex_ch=tse_a.tw|tse_b.tw 批次查詢。
// 與 STOCK_DAY 同套件 (twse),共用 cleanCell / parseFloat 清洗;baseURL / now 可注入供 httptest。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// defaultMISBaseURL 為 TWSE MIS 即時報價端點。
const defaultMISBaseURL = "https://mis.twse.com.tw/stock/api/getStockInfo.jsp"

// misPrimeURL 為先行 GET 以建立 session cookie 的頁面 (提高直接呼叫 API 的成功率)。
const misPrimeURL = "https://mis.twse.com.tw/stock/index.jsp"

// misExchPrefix 為上市 (TSE) 證券的 ex_ch 前綴 (追蹤的 ETF 皆為上市)。
const misExchPrefix = "tse_"

// browserUA 模擬瀏覽器 User-Agent,符合 MIS 對請求標頭的期待。
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// Quote 為單一股票的即時報價子集 (僅取線上決策需要的欄位)。
type Quote struct {
	StockID string  // 證券代號 (c)
	Name    string  // 證券名稱 (n)
	Open    float64 // 開盤價 (o);尚未成交時為 0
	Date    string  // 交易日期 (d, "YYYYMMDD")
}

// RealtimeClient 為 TWSE MIS 即時報價客戶端 (帶 cookie jar 與瀏覽器 UA)。零值不可用,請用 NewRealtimeClient。
type RealtimeClient struct {
	httpClient *http.Client
	baseURL    string
	now        func() time.Time // 可注入「現在時間」,預設 time.Now;供 FetchOpens 判斷資料是否為當日
	loc        *time.Location   // 交易所時區 (Asia/Taipei),供「當日」判斷
	primed     bool             // 是否已先行 GET 建立 cookie
}

// RealtimeOption 以函式選項模式設定 RealtimeClient。
type RealtimeOption func(*RealtimeClient)

// WithRealtimeBaseURL 覆寫 MIS API 端點,主要供 httptest 注入假伺服器。
func WithRealtimeBaseURL(baseURL string) RealtimeOption {
	return func(c *RealtimeClient) {
		if baseURL != "" {
			c.baseURL = baseURL
			c.primed = true // 測試端點不需 cookie priming
		}
	}
}

// WithRealtimeHTTPClient 覆寫底層 http.Client,供注入自訂 timeout / transport 或測試替身。
func WithRealtimeHTTPClient(httpClient *http.Client) RealtimeOption {
	return func(c *RealtimeClient) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithRealtimeNow 覆寫「現在時間」來源,供測試固定「今天」以驗證當日資料過濾。
func WithRealtimeNow(now func() time.Time) RealtimeOption {
	return func(c *RealtimeClient) {
		if now != nil {
			c.now = now
		}
	}
}

// NewRealtimeClient 建立 MIS 即時報價客戶端;預設端點為真實 MIS、timeout 30s、時區 Asia/Taipei。
func NewRealtimeClient(opts ...RealtimeOption) *RealtimeClient {
	// 以 cookie jar 初始化 http.Client,使 priming 取得的 session cookie 能在後續請求帶上。
	jar, _ := cookiejar.New(nil)
	// 載入台灣時區;失敗時退回固定 UTC+8,確保「當日」判斷仍正確。
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	c := &RealtimeClient{
		httpClient: &http.Client{Timeout: 30 * time.Second, Jar: jar},
		baseURL:    defaultMISBaseURL,
		now:        time.Now,
		loc:        loc,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchOpens 批次抓取各追蹤股的即時報價,回傳「資料日期為當日 (Asia/Taipei) 且開盤價>0」的開盤價 map。
// 尚未成交 (o 為空 / "-") 或資料仍停在前一交易日的股票不納入回傳 → 呼叫端視為「尚未就緒」並重試。
func (c *RealtimeClient) FetchOpens(ctx context.Context, stockIDs []string) (map[string]float64, error) {
	quotes, err := c.FetchQuotes(ctx, stockIDs)
	if err != nil {
		return nil, err
	}
	// 以台灣時區的今日日期 (YYYYMMDD) 過濾,避免誤用前一交易日的隔夜 snapshot。
	today := c.now().In(c.loc).Format("20060102")
	out := make(map[string]float64, len(quotes))
	for id, q := range quotes {
		if q.Date == today && q.Open > 0 {
			out[id] = q.Open
		}
	}
	return out, nil
}

// FetchQuotes 以單一 HTTP 請求批次抓取多檔即時報價並解析 msgArray (守 rate limit:一次查完所有股)。
func (c *RealtimeClient) FetchQuotes(ctx context.Context, stockIDs []string) (map[string]Quote, error) {
	if len(stockIDs) == 0 {
		return map[string]Quote{}, nil
	}

	// 先行 GET index 頁建立 session cookie (best-effort;失敗不致命)。
	c.prime(ctx)

	// 組裝 ex_ch=tse_<id>.tw|... 批次查詢字串與完整 URL。
	chans := make([]string, 0, len(stockIDs))
	for _, id := range stockIDs {
		chans = append(chans, misExchPrefix+id+".tw")
	}
	url := fmt.Sprintf("%s?ex_ch=%s&json=1&delay=0", c.baseURL, strings.Join(chans, "|"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("twse-mis: build request: %w", err)
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Referer", misPrimeURL)

	// 發送請求並驗證 HTTP 狀態碼。
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twse-mis: http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("twse-mis: unexpected status %d", resp.StatusCode)
	}

	// 解碼 JSON 並轉為 stockID → Quote map。
	var payload misResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("twse-mis: decode json: %w", err)
	}
	out := make(map[string]Quote, len(payload.MsgArray))
	for _, m := range payload.MsgArray {
		if m.C == "" {
			continue
		}
		out[m.C] = Quote{
			StockID: m.C,
			Name:    m.N,
			Open:    parseFloat(cleanCell(m.O)),
			Date:    strings.TrimSpace(m.D),
		}
	}
	return out, nil
}

// prime 先行 GET index 頁以取得 session cookie (僅執行一次;任何失敗皆忽略,不影響後續查詢)。
func (c *RealtimeClient) prime(ctx context.Context) {
	if c.primed {
		return
	}
	c.primed = true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, misPrimeURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// misResponse 對應 MIS getStockInfo.jsp 回應的頂層結構 (僅取 msgArray)。
type misResponse struct {
	MsgArray []misQuote `json:"msgArray"`
}

// misQuote 對應 msgArray 內單一股票的報價欄位 (僅取線上決策需要者)。
type misQuote struct {
	C string `json:"c"` // 證券代號
	N string `json:"n"` // 證券名稱
	O string `json:"o"` // 開盤價
	D string `json:"d"` // 交易日期 YYYYMMDD
}
