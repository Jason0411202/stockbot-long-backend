package discord

import (
	"io"
	"testing"

	"main/app_context"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

// discord_test.go 驗證不需真實連線即可判定的失敗保護 (缺 token、未初始化 session、缺 channel id)。

func testCtx() *app_context.AppContext {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return &app_context.AppContext{Log: log}
}

func TestInitDiscord_MissingToken(t *testing.T) {
	// Arrange — 無 DISCORD_BOT_TOKEN。
	t.Setenv("DISCORD_BOT_TOKEN", "")
	// Act + Assert
	if err := InitDiscord(testCtx()); err == nil {
		t.Fatalf("expected error when DISCORD_BOT_TOKEN missing")
	}
}

func TestSendEmbed_NilSession(t *testing.T) {
	// Arrange — session 尚未初始化。
	appCtx := testCtx() // Dg == nil
	// Act + Assert
	if err := SendEmbedDiscordMessage(appCtx, "t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when Dg is nil")
	}
}

func TestSendEmbed_MissingChannelID(t *testing.T) {
	// Arrange — session 已設,但缺 DISCORD_BOT_CHANNELID。
	t.Setenv("DISCORD_BOT_CHANNELID", "")
	appCtx := testCtx()
	appCtx.Dg = &discordgo.Session{}

	// Act + Assert — 在打網路前就因缺 channel id 而回錯。
	if err := SendEmbedDiscordMessage(appCtx, "t", "m", 0x00ff00); err == nil {
		t.Fatalf("expected error when channel id missing")
	}
}
