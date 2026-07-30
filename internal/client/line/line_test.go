// internal/client/line/line_test.go 驗證 LINE client 的建構防呆與 push message 請求行為。
package line

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClient_MissingCredentials 驗證缺少 token 或收件者 user ID 時回傳錯誤。
func TestNewClient_MissingCredentials(t *testing.T) {
	cases := []struct {
		name  string
		token string
		to    string
	}{
		{name: "missing token", token: "", to: "U0123456789abcdef0123456789abcdef"},
		{name: "missing to", token: "token", to: ""},
		{name: "missing both", token: "", to: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewClient(tc.token, tc.to, nil); err == nil {
				t.Fatal("NewClient 應回傳錯誤, got nil")
			}
		})
	}
}

// TestPushText_Success 驗證 PushText 對 push message API 發出正確的路徑、認證標頭與 JSON 主體。
func TestPushText_Success(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody pushRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := lineBaseURL
	lineBaseURL = srv.URL
	defer func() { lineBaseURL = orig }()

	c, err := NewClient("test-token", "U0123", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.PushText("🟥 買入成交 — 00631L"); err != nil {
		t.Fatalf("PushText: %v", err)
	}

	if gotPath != "/v2/bot/message/push" {
		t.Errorf("path = %q, want /v2/bot/message/push", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody.To != "U0123" {
		t.Errorf("to = %q, want U0123", gotBody.To)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Type != "text" || gotBody.Messages[0].Text != "🟥 買入成交 — 00631L" {
		t.Errorf("messages = %+v, want 單則 text 訊息", gotBody.Messages)
	}
}

// TestPushText_Truncate 驗證超過單則上限的文字被截斷後仍成功送出。
func TestPushText_Truncate(t *testing.T) {
	var gotBody pushRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := lineBaseURL
	lineBaseURL = srv.URL
	defer func() { lineBaseURL = orig }()

	c, err := NewClient("test-token", "U0123", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.PushText(strings.Repeat("字", maxTextLen+100)); err != nil {
		t.Fatalf("PushText: %v", err)
	}
	if got := len([]rune(gotBody.Messages[0].Text)); got != maxTextLen {
		t.Errorf("truncated len = %d, want %d", got, maxTextLen)
	}
}

// TestPushText_APIError 驗證非 2xx 回應時回傳含狀態碼的錯誤。
func TestPushText_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	defer srv.Close()

	orig := lineBaseURL
	lineBaseURL = srv.URL
	defer func() { lineBaseURL = orig }()

	c, err := NewClient("bad-token", "U0123", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.PushText("hello")
	if err == nil {
		t.Fatal("PushText 應回傳錯誤, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, 應包含狀態碼 401", err)
	}
}

// TestPushText_NilClient 驗證 nil client 呼叫 PushText 回傳錯誤而非 panic。
func TestPushText_NilClient(t *testing.T) {
	var c *Client
	if err := c.PushText("hello"); err == nil {
		t.Fatal("nil client 應回傳錯誤, got nil")
	}
}
