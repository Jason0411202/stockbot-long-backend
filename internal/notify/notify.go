// internal/notify/notify.go 將多個通知管道 (Discord / LINE) 聚合為單一發送介面。
//
// Fanout 實作 service.Notifier,把同一則通知同步發送到所有已設定的管道;
// Line 則把中性通知 DTO 轉為純文字後交由 LINE client 推播。
package notify

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
)

// Channel 是單一通知管道需實作的最小介面,與 service.Notifier 同構
// (於本套件重複宣告,避免 notify → service 的反向依賴)。
type Channel interface {
	SendEmbed(title, message string, color int) error
	SendTradeEmbed(n discord.TradeNotification) error
}

// Fanout 將通知同時發送到多個管道;沒有任何管道時所有發送皆為 no-op。
type Fanout struct {
	channels []Channel
}

// NewFanout 建立 Fanout,略過 nil 管道。
func NewFanout(channels ...Channel) *Fanout {
	// 過濾掉未設定 (nil) 的管道,保留實際可發送者。
	kept := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		if ch != nil {
			kept = append(kept, ch)
		}
	}
	return &Fanout{channels: kept}
}

// SendEmbed 將單行描述通知發送到所有管道,回傳所有失敗管道的聚合錯誤。
func (f *Fanout) SendEmbed(title, message string, color int) error {
	var errs []error
	for _, ch := range f.channels {
		if err := ch.SendEmbed(title, message, color); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SendTradeEmbed 將成交通知發送到所有管道,回傳所有失敗管道的聚合錯誤。
func (f *Fanout) SendTradeEmbed(n discord.TradeNotification) error {
	var errs []error
	for _, ch := range f.channels {
		if err := ch.SendTradeEmbed(n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// TextPusher 是 Line 管道所需的最小推播介面,由 *line.Client 實作。
type TextPusher interface {
	PushText(text string) error
}

// Line 把通知內容轉為純文字後經 LINE 推播;實作 Channel。
type Line struct {
	pusher TextPusher
}

// NewLine 以任一 TextPusher (通常為 *line.Client) 建立 LINE 通知管道。
func NewLine(pusher TextPusher) *Line {
	return &Line{pusher: pusher}
}

// SendEmbed 將單行描述通知以「標題 + 換行 + 內容」的純文字推播。
func (l *Line) SendEmbed(title, message string, color int) error {
	// 管道未初始化時提前回傳錯誤。
	if l == nil || l.pusher == nil {
		return fmt.Errorf("SendEmbed() 失敗, LINE 通知管道尚未初始化")
	}
	return l.pusher.PushText(title + "\n" + message)
}

// SendTradeEmbed 將成交通知的標題、逐欄位內容與頁尾組成多行純文字後推播。
func (l *Line) SendTradeEmbed(n discord.TradeNotification) error {
	// 管道未初始化時提前回傳錯誤。
	if l == nil || l.pusher == nil {
		return fmt.Errorf("SendTradeEmbed() 失敗, LINE 通知管道尚未初始化")
	}

	// 以「標題 → 各欄位 name: value → 頁尾」的順序組裝多行訊息。
	var b strings.Builder
	b.WriteString(n.Title)
	for _, f := range n.Fields {
		b.WriteString(fmt.Sprintf("\n%s: %s", f.Name, f.Value))
	}
	if n.Footer != "" {
		b.WriteString("\n" + n.Footer)
	}
	return l.pusher.PushText(b.String())
}
