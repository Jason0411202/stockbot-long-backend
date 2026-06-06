package discord

import (
	"io"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

// discord_test.go 驗證不需真實連線即可判定的失敗保護 (缺 token、未初始化 session、缺 channel id)。

func testLog() *logrus.Logger {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return log
}

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

func TestSendEmbed_NilClient(t *testing.T) {
	// Arrange — nil client (session 尚未初始化)。
	var c *Client
	// Act + Assert
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when client is nil")
	}
}

func TestSendEmbed_NilSession(t *testing.T) {
	// Arrange — zero-value client，session == nil。
	c := &Client{log: testLog()}
	// Act + Assert
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when session is nil")
	}
}

func TestSendEmbed_MissingChannelID(t *testing.T) {
	// Arrange — session 已設,但缺 channel id。
	c := &Client{session: &discordgo.Session{}, log: testLog()}
	// Act + Assert — 在打網路前就因缺 channel id 而回錯。
	if err := c.SendEmbed("t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when channel id missing")
	}
}

func TestClose_NilClient(t *testing.T) {
	// Arrange — nil client。
	var c *Client
	// Act + Assert — nil-guard 應回 nil。
	if err := c.Close(); err != nil {
		t.Fatalf("expected nil error closing nil client, got %v", err)
	}
}

func TestClose_NilSession(t *testing.T) {
	// Arrange — zero-value client，session == nil。
	c := &Client{log: testLog()}
	// Act + Assert
	if err := c.Close(); err != nil {
		t.Fatalf("expected nil error closing client with nil session, got %v", err)
	}
}
