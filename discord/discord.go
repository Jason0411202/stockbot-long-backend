package discord

import (
	"fmt"
	"main/app_context"
	"os"

	"github.com/bwmarrin/discordgo"
)

func InitDiscord(appCtx *app_context.AppContext) error {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" { //若 .env 中的 DISCORD_BOT_TOKEN 不存在, 或是值為空
		err := fmt.Errorf("InitDiscord() 失敗, 找不到環境變數 DISCORD_BOT_TOKEN, 請確認環境變數設定無誤")
		appCtx.Log.Error(err)
		return err
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		appCtx.Log.Error("InitDiscord() 失敗, 建立 Discord session 失敗: %w", err)
		return err
	}

	err = dg.Open()
	if err != nil {
		appCtx.Log.Error("InitDiscord() 失敗, 無法連線至 Discord: %w", err)
		return err
	}

	appCtx.Log.Info("成功連線至 Discord")
	appCtx.Dg = dg

	return nil
}
func SendEmbedDiscordMessage(appCtx *app_context.AppContext, title, message string, color int) error {
	if appCtx.Dg == nil {
		err := fmt.Errorf("SendEmbedDiscordMessage() 失敗, Discord session 尚未初始化")
		appCtx.Log.Error(err)
		return err
	}
	discord_channel_id := os.Getenv("DISCORD_BOT_CHANNELID")

	//若 .env 中的 DISCORD_BOT_CHANNELID 不存在, 或是值為空
	if discord_channel_id == "" {
		err := fmt.Errorf("InitDiscord() 失敗, 找不到環境變數 DISCORD_BOT_CHANNELID, 請確認環境變數設定無誤")
		appCtx.Log.Error(err)
		return err
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: message,
		Color:       color,
	}

	_, err := appCtx.Dg.ChannelMessageSendEmbed(discord_channel_id, embed)
	if err != nil {
		appCtx.Log.Errorf("SendDiscordEmbed() 發送訊息失敗: %v", err)
		return err
	}

	return nil
}
