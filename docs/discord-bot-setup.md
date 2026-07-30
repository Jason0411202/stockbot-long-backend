# Discord Bot 設定懶人包

照著 5 步做完,交易通知就會進你的 Discord 頻道。Discord 通知為**可選功能**:
`.env` 的 `DISCORD_BOT_TOKEN` / `DISCORD_BOT_CHANNELID` 留空即代表不使用,系統照常運作。

本系統只用 bot 來「發訊息」(啟動通知 + 每筆買賣成交 embed),不讀取任何訊息,
因此**不需要開任何 Privileged Gateway Intents**。

## Step 1 · 建立 Application 與 Bot

1. 打開 [Discord Developer Portal](https://discord.com/developers/applications),用你的 Discord 帳號登入。
2. 右上角 **New Application** → 取個名字 (例 `stockbot`) → 同意條款 → **Create**。
3. 左側選 **Bot** 分頁 (新版建立 Application 時會自動附帶一個 bot user,這裡可改頭像與顯示名稱)。

## Step 2 · 取得 Bot Token

1. 在 **Bot** 分頁按 **Reset Token** → 確認 (有開 2FA 會要求輸入驗證碼)。
2. 複製顯示的 token —— **只會顯示這一次**,離開頁面就看不到了,忘記就再 Reset 一次。
3. 填到 `.env` 的 `DISCORD_BOT_TOKEN`。

> ⚠️ Token 等同 bot 的密碼:不要 commit 進版控、不要貼在公開的地方。外洩就立刻 Reset Token 換新。

## Step 3 · 把 Bot 邀進你的伺服器

1. 左側選 **OAuth2** → **URL Generator**。
2. **Scopes** 勾 `bot`。
3. 下方 **Bot Permissions** 勾 `Send Messages`(以及 `Embed Links`,發美化的成交通知需要)。
4. 複製最下方產生的 URL,貼到瀏覽器開啟 → 選擇你的伺服器 → **授權**。
   (沒有自己的伺服器就先在 Discord 建一個,只有你一人也行。)

## Step 4 · 取得頻道 ID

1. Discord App → **使用者設定 (齒輪)** → **進階** → 打開 **開發者模式**。
2. 回到你的伺服器,對要接收通知的文字頻道按**右鍵** → **複製頻道 ID**。
3. 填到 `.env` 的 `DISCORD_BOT_CHANNELID`。

## Step 5 · 填入 .env 並重啟

```bash
DISCORD_BOT_TOKEN=你的 bot token
DISCORD_BOT_CHANNELID=你的頻道 ID
```

- 本機:`docker compose up -d app`(或重跑 `go run ./cmd/server`)。
- 正式機 (走 CI/CD 部署):`.env` 的單一來源是 GitHub 的 `DEPLOY_ENV_FILE` secret,
  把上面兩行加進該 secret 內容後重跑部署即可 (見 [cicd-k8s.md](cicd-k8s.md))。

啟動成功時,該頻道會收到一則「📢 SYSTEM 長線股票模擬交易系統通知 bot 順利啟動」訊息;
之後每筆買賣成交都會收到附決策理由的 embed 通知。

## 疑難排解

| 症狀 | 原因與解法 |
| --- | --- |
| log 出現 `缺少 Discord bot token` | `.env` 沒填 `DISCORD_BOT_TOKEN`,或容器沒吃到新 `.env` (重啟 app 容器) |
| log 出現 `無法連線至 Discord` | token 錯誤或已被 Reset,重新複製一次 |
| 啟動訊息沒出現在頻道 | bot 沒被邀進該伺服器、頻道 ID 複製錯,或 bot 在該頻道沒有發言權限 |
| 通知失敗但交易正常 | 屬預期行為:通知失敗僅記 log,不影響成交與帳本 |
