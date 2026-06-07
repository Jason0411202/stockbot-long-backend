// Package discord provides a typed Discord client decoupled from app_context.
//
// It mirrors the behaviour of the legacy discord package (InitDiscord /
// SendEmbedDiscordMessage) but exposes it as an explicit *Client whose
// dependencies (token, channel ID, logger) are passed in by the caller. This
// keeps the package free of any app_context import so it can be cut over to
// incrementally (strangler pattern).
package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

// Client wraps an authenticated discordgo session bound to a single channel.
type Client struct {
	session   *discordgo.Session
	channelID string
	log       *logrus.Logger
}

// NewClient builds a Discord client from a bot token and target channel ID.
//
// It mirrors the legacy InitDiscord flow: an empty token is rejected, a session
// is created via discordgo.New("Bot "+token) and opened with session.Open().
// Any failure is wrapped with context. The channel ID is taken as a constructor
// parameter so callers pass os.Getenv("DISCORD_BOT_CHANNELID") themselves,
// keeping this package independent of the environment.
func NewClient(token, channelID string, log *logrus.Logger) (*Client, error) {
	// token 為空時立即回傳錯誤,避免以空字串建立無效 session。
	if token == "" {
		err := fmt.Errorf("NewClient() 失敗, 缺少 Discord bot token, 請確認環境變數設定無誤")
		if log != nil {
			log.Error(err)
		}
		return nil, err
	}

	// 以 "Bot <token>" 格式建立 discordgo session。
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		wrapped := fmt.Errorf("NewClient() 失敗, 建立 Discord session 失敗: %w", err)
		if log != nil {
			log.Error(wrapped)
		}
		return nil, wrapped
	}

	// 開啟 WebSocket 連線;連線失敗時回傳包裝錯誤。
	if err := session.Open(); err != nil {
		wrapped := fmt.Errorf("NewClient() 失敗, 無法連線至 Discord: %w", err)
		if log != nil {
			log.Error(wrapped)
		}
		return nil, wrapped
	}

	// 連線成功後記錄 Info 訊息並回傳已初始化的 Client。
	if log != nil {
		log.Info("成功連線至 Discord")
	}

	return &Client{
		session:   session,
		channelID: channelID,
		log:       log,
	}, nil
}

// SendEmbed sends an embed message (title, description, colour) to the client's
// configured channel. It guards against an uninitialised session and builds the
// same discordgo.MessageEmbed structure as the legacy SendEmbedDiscordMessage.
func (c *Client) SendEmbed(title, message string, color int) error {
	// session 未初始化時提前回傳錯誤。
	if c == nil || c.session == nil {
		err := fmt.Errorf("SendEmbed() 失敗, Discord session 尚未初始化")
		if c != nil && c.log != nil {
			c.log.Error(err)
		}
		return err
	}

	// channelID 為空時無法寄送,提前回傳錯誤。
	if c.channelID == "" {
		err := fmt.Errorf("SendEmbed() 失敗, 缺少 Discord channel id, 請確認環境變數設定無誤")
		if c.log != nil {
			c.log.Error(err)
		}
		return err
	}

	// 組裝 Embed 訊息結構並送出至目標頻道。
	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: message,
		Color:       color,
	}

	if _, err := c.session.ChannelMessageSendEmbed(c.channelID, embed); err != nil {
		wrapped := fmt.Errorf("SendEmbed() 發送訊息失敗: %w", err)
		if c.log != nil {
			c.log.Error(wrapped)
		}
		return wrapped
	}

	return nil
}

// Close closes the underlying Discord session. It is a no-op when the client or
// its session is nil.
func (c *Client) Close() error {
	// client 或 session 為 nil 時直接回傳,不做任何操作。
	if c == nil || c.session == nil {
		return nil
	}
	// 關閉底層 WebSocket 連線,失敗時回傳包裝錯誤。
	if err := c.session.Close(); err != nil {
		return fmt.Errorf("Close() 失敗, 關閉 Discord session 失敗: %w", err)
	}
	return nil
}
