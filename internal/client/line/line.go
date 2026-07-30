// internal/client/line/line.go 提供 LINE Messaging API 推播通知的輕量 client。
//
// 僅使用 push message API 將文字訊息推播給單一收件者 (user ID),
// 不處理 webhook 回覆,因此不需要 channel secret;以標準函式庫 net/http 實作,無額外相依。
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

// Client 包裝以 channel access token 認證、推播給單一收件者的 LINE Messaging API client。
type Client struct {
	token string
	to    string
	httpc *http.Client
	log   *logrus.Logger
}

// NewClient 以 channel access token 與收件者 user ID 建立 LINE client。
// 任一參數為空時回傳錯誤,由呼叫端決定是否視為「未啟用 LINE 通知」。
func NewClient(token, to string, log *logrus.Logger) (*Client, error) {
	// 缺少必要憑證時立即回傳錯誤,不建立無效 client。
	if token == "" || to == "" {
		return nil, fmt.Errorf("NewClient() 失敗, 缺少 LINE channel access token 或收件者 user ID, 請確認環境變數設定無誤")
	}

	return &Client{
		token: token,
		to:    to,
		httpc: &http.Client{Timeout: 10 * time.Second},
		log:   log,
	}, nil
}

// textMessage 對應 push message API 的單一文字訊息物件。
type textMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// pushRequest 對應 push message API 的請求主體。
type pushRequest struct {
	To       string        `json:"to"`
	Messages []textMessage `json:"messages"`
}

// PushText 以 push message API 將一則文字訊息推播給設定的收件者。
func (c *Client) PushText(text string) error {
	// client 未初始化時提前回傳錯誤。
	if c == nil || c.httpc == nil {
		return fmt.Errorf("PushText() 失敗, LINE client 尚未初始化")
	}

	// 超過單則上限時截斷,避免整則被 LINE API 拒絕。
	if runes := []rune(text); len(runes) > maxTextLen {
		text = string(runes[:maxTextLen])
	}

	// 組裝 push message 請求 JSON。
	body, err := json.Marshal(pushRequest{To: c.to, Messages: []textMessage{{Type: "text", Text: text}}})
	if err != nil {
		return fmt.Errorf("PushText() 失敗, 序列化請求錯誤: %w", err)
	}

	// 建立帶 Bearer token 的 POST 請求並送出。
	req, err := http.NewRequest(http.MethodPost, lineBaseURL+"/v2/bot/message/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("PushText() 失敗, 建立請求錯誤: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("PushText() 發送請求失敗: %w", err)
	}
	defer resp.Body.Close()

	// 非 2xx 視為失敗,附帶回應內容片段以利除錯。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("PushText() 失敗, LINE API 回應 %d: %s", resp.StatusCode, string(payload))
	}
	return nil
}
