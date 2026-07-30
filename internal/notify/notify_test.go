// internal/notify/notify_test.go 驗證 Fanout 的多管道發送 / 錯誤聚合與 Line 管道的文字格式化。
package notify

import (
	"errors"
	"strings"
	"testing"

	"github.com/Jason0411202/stockbot-long-backend/internal/client/discord"
)

// fakeChannel 記錄收到的通知並可注入錯誤,供 Fanout 測試使用。
type fakeChannel struct {
	embeds []string
	trades []discord.TradeNotification
	err    error
}

// SendEmbed 記錄一次單行通知呼叫並回傳注入的錯誤。
func (f *fakeChannel) SendEmbed(title, message string, color int) error {
	f.embeds = append(f.embeds, title+"|"+message)
	return f.err
}

// SendTradeEmbed 記錄一次成交通知呼叫並回傳注入的錯誤。
func (f *fakeChannel) SendTradeEmbed(n discord.TradeNotification) error {
	f.trades = append(f.trades, n)
	return f.err
}

// fakePusher 記錄群發文字並可注入錯誤,供 Line 管道測試使用。
type fakePusher struct {
	texts []string
	err   error
}

// BroadcastText 記錄一次群發呼叫並回傳注入的錯誤。
func (f *fakePusher) BroadcastText(text string) error {
	f.texts = append(f.texts, text)
	return f.err
}

// TestFanout_SendsToAllChannels 驗證通知同步發送到所有管道且 nil 管道被略過。
func TestFanout_SendsToAllChannels(t *testing.T) {
	a, b := &fakeChannel{}, &fakeChannel{}
	f := NewFanout(a, nil, b)

	if err := f.SendEmbed("T", "M", 1); err != nil {
		t.Fatalf("SendEmbed: %v", err)
	}
	if err := f.SendTradeEmbed(discord.TradeNotification{Title: "trade"}); err != nil {
		t.Fatalf("SendTradeEmbed: %v", err)
	}

	for name, ch := range map[string]*fakeChannel{"a": a, "b": b} {
		if len(ch.embeds) != 1 || len(ch.trades) != 1 {
			t.Errorf("channel %s 收到 embeds=%d trades=%d, want 各 1", name, len(ch.embeds), len(ch.trades))
		}
	}
}

// TestFanout_AggregatesErrors 驗證單一管道失敗不阻擋其他管道,且錯誤被聚合回傳。
func TestFanout_AggregatesErrors(t *testing.T) {
	bad := &fakeChannel{err: errors.New("discord down")}
	good := &fakeChannel{}
	f := NewFanout(bad, good)

	err := f.SendEmbed("T", "M", 1)
	if err == nil {
		t.Fatal("SendEmbed 應回傳聚合錯誤, got nil")
	}
	if !strings.Contains(err.Error(), "discord down") {
		t.Errorf("error = %v, 應包含失敗管道的錯誤", err)
	}
	if len(good.embeds) != 1 {
		t.Errorf("正常管道應仍收到通知, got %d", len(good.embeds))
	}
}

// TestFanout_EmptyIsNoop 驗證沒有任何管道時發送為 no-op 且不回傳錯誤。
func TestFanout_EmptyIsNoop(t *testing.T) {
	f := NewFanout()
	if err := f.SendEmbed("T", "M", 1); err != nil {
		t.Fatalf("SendEmbed: %v", err)
	}
	if err := f.SendTradeEmbed(discord.TradeNotification{}); err != nil {
		t.Fatalf("SendTradeEmbed: %v", err)
	}
}

// TestLine_SendEmbed 驗證單行通知被組成「標題 + 換行 + 內容」推播。
func TestLine_SendEmbed(t *testing.T) {
	p := &fakePusher{}
	l := NewLine(p)
	if err := l.SendEmbed("📢 SYSTEM", "系統啟動", 0x00ff00); err != nil {
		t.Fatalf("SendEmbed: %v", err)
	}
	if len(p.texts) != 1 || p.texts[0] != "📢 SYSTEM\n系統啟動" {
		t.Errorf("texts = %q, want 標題+換行+內容", p.texts)
	}
}

// TestLine_SendTradeEmbed 驗證成交通知被展開為「標題 → 欄位 → 頁尾」的多行文字。
func TestLine_SendTradeEmbed(t *testing.T) {
	p := &fakePusher{}
	l := NewLine(p)
	n := discord.TradeNotification{
		Title: "🟥 買入成交 — 00631L",
		Fields: []discord.TradeField{
			{Name: "股票", Value: "00631L"},
			{Name: "股數", Value: "100 股"},
		},
		Footer: "成交日 2026-07-30｜開盤價即時決策",
	}
	if err := l.SendTradeEmbed(n); err != nil {
		t.Fatalf("SendTradeEmbed: %v", err)
	}
	want := "🟥 買入成交 — 00631L\n股票: 00631L\n股數: 100 股\n成交日 2026-07-30｜開盤價即時決策"
	if len(p.texts) != 1 || p.texts[0] != want {
		t.Errorf("texts = %q, want %q", p.texts, want)
	}
}

// TestLine_PusherError 驗證底層推播失敗時錯誤被原樣回傳。
func TestLine_PusherError(t *testing.T) {
	p := &fakePusher{err: errors.New("line api 401")}
	l := NewLine(p)
	if err := l.SendEmbed("T", "M", 1); err == nil || !strings.Contains(err.Error(), "line api 401") {
		t.Errorf("error = %v, 應回傳底層推播錯誤", err)
	}
}
