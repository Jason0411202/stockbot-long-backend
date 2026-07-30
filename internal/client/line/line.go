// internal/client/line/line.go 提供 LINE Messaging API 群發通知的輕量 client。
//
// 僅使用 broadcast API 將文字訊息群發給「所有加官方帳號好友的使用者」,
// 不處理 webhook 回覆,因此不需要 channel secret 與收件者名單;
// 以標準函式庫 net/http 實作,無額外相依。
package line

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// lineBaseURL 為 LINE Messaging API 的基底位址;測試時可代換為 httptest server。
var lineBaseURL = "https://api.line.me"

// maxTextLen 為 LINE 單則文字訊息的長度上限 (字元數),超過時截斷保留前段。
const maxTextLen = 5000

// Client 包裝以 channel access token 認證、對全體好友群發的 LINE Messaging API client。
type Client struct {
	token string
	httpc *http.Client
	log   *logrus.Entry
}

// NewClient 以 channel access token 建立 LINE client。
// token 為空時回傳錯誤,由呼叫端決定是否視為「未啟用 LINE 通知」。
func NewClient(token string, log *logrus.Logger) (*Client, error) {
	// 缺少必要憑證時立即回傳錯誤,不建立無效 client。
	if token == "" {
		return nil, fmt.Errorf("NewClient() 失敗, 缺少 LINE channel access token, 請確認環境變數設定無誤")
	}

	// 以 component 欄位標記本 client 的所有 log;呼叫端未提供 logger 時維持 nil。
	var entry *logrus.Entry
	if log != nil {
		entry = log.WithField("component", "line")
	}

	return &Client{
		token: token,
		httpc: &http.Client{Timeout: 10 * time.Second},
		log:   entry,
	}, nil
}

// textMessage 對應 broadcast API 的單一文字訊息物件。
type textMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// broadcastRequest 對應 broadcast API 的請求主體 (不指定收件者,發給全體好友)。
type broadcastRequest struct {
	Messages []textMessage `json:"messages"`
}

// BroadcastText 以 broadcast API 將一則文字訊息群發給所有加官方帳號好友的使用者。
func (c *Client) BroadcastText(text string) error {
	// client 未初始化時提前回傳錯誤。
	if c == nil || c.httpc == nil {
		return fmt.Errorf("BroadcastText() 失敗, LINE client 尚未初始化")
	}

	// 超過單則上限時截斷,避免整則被 LINE API 拒絕。
	if runes := []rune(text); len(runes) > maxTextLen {
		text = string(runes[:maxTextLen])
	}

	// 組裝 broadcast 請求 JSON。
	body, err := json.Marshal(broadcastRequest{Messages: []textMessage{{Type: "text", Text: text}}})
	if err != nil {
		return fmt.Errorf("BroadcastText() 失敗, 序列化請求錯誤: %w", err)
	}

	// 建立帶 Bearer token 的 POST 請求並送出。
	req, err := http.NewRequest(http.MethodPost, lineBaseURL+"/v2/bot/message/broadcast", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("BroadcastText() 失敗, 建立請求錯誤: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("BroadcastText() 發送請求失敗: %w", err)
	}
	defer resp.Body.Close()

	// 非 2xx 視為失敗,附帶回應內容片段以利除錯。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("BroadcastText() 失敗, LINE API 回應 %d: %s", resp.StatusCode, string(payload))
	}
	return nil
}
