// internal/client/discord/discord_test.go 以 nil 與缺漏設定情境覆蓋 Discord client 的防呆分支。
package discord

import (
	"io"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

// discord_test.go 驗證不需真實連線即可判定的失敗保護 (缺 token、未初始化 session、缺 channel id)。

// testLog 建立一個將輸出丟棄的 logrus.Logger，供測試注入使用。
func testLog() *logrus.Logger {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return log
}

// TestNewClient_MissingToken 驗證 token 為空字串時 NewClient 回傳錯誤且不發起網路連線。
func TestNewClient_MissingToken(t *testing.T) {
	// Arrange + Act — 空 token 應在打網路前回錯。
	c, err := NewClient("", "channel", testLog())
	// Assert
	if err == nil {
		t.Fatalf("expected error when token is empty")
	}
	if c != nil {
		t.Fatalf("expected nil client when token is empty, got %#v", c)
	}
}

// TestSendEmbed_NilClient 驗證對 nil Client 呼叫 SendEmbed 時回傳錯誤。
func TestSendEmbed_NilClient(t *testing.T) {
	// Arrange — nil client (session 尚未初始化)。
	var c *Client
	// Act + Assert
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when client is nil")
	}
}

// TestSendEmbed_NilSession 驗證 session 尚未初始化 (nil) 時 SendEmbed 回傳錯誤。
func TestSendEmbed_NilSession(t *testing.T) {
	// Arrange — zero-value client，session == nil。
	c := &Client{log: testLog()}
	// Act + Assert
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when session is nil")
	}
}

// TestSendEmbed_MissingChannelID 驗證 channel id 為空時 SendEmbed 在發起網路請求前即回傳錯誤。
func TestSendEmbed_MissingChannelID(t *testing.T) {
	// Arrange — session 已設,但缺 channel id。
	c := &Client{session: &discordgo.Session{}, log: testLog()}
	// Act + Assert — 在打網路前就因缺 channel id 而回錯。
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when channel id missing")
	}
}

// TestClose_NilClient 驗證對 nil Client 呼叫 Close 時回傳 nil 而非 panic。
func TestClose_NilClient(t *testing.T) {
	// Arrange — nil client。
	var c *Client
	// Act + Assert — nil-guard 應回 nil。
	if err := c.Close(); err != nil {
		t.Fatalf("expected nil error closing nil client, got %v", err)
	}
}

// TestClose_NilSession 驗證 session 為 nil 時 Close 回傳 nil 而非 panic。
func TestClose_NilSession(t *testing.T) {
	// Arrange — zero-value client，session == nil。
	c := &Client{log: testLog()}
	// Act + Assert
	if err := c.Close(); err != nil {
		t.Fatalf("expected nil error closing client with nil session, got %v", err)
	}
}
