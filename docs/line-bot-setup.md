# LINE Bot 設定懶人包

照著 4 步做完,交易通知就會推播到你的 LINE。LINE 通知為**可選功能**:
`.env` 的 `LINE_CHANNEL_ACCESS_TOKEN` / `LINE_NOTIFY_TO` 留空即代表不使用,系統照常運作。

本系統只用 Messaging API 的 **push message** 對你個人推播 (啟動通知 + 每筆買賣成交),
**不接收訊息、不需要 webhook、也用不到 channel secret**,設定比一般 LINE bot 簡單很多。

## Step 1 · 建官方帳號、開 Messaging API

1. 到 [LINE Official Account Manager](https://manager.line.biz/) 用 LINE 帳號登入 → **建立帳號**(分類隨意選即可)。
2. 進該帳號 → **設定** → **Messaging API** → **啟用 Messaging API** → 建立或選一個 Provider。
3. (可選) **設定** → **回應設定**:自動回應訊息改為**停用**,避免你加好友後收到罐頭訊息。
   Webhook 不用開 —— 本系統只推播、不接收。

## Step 2 · 抄兩個值

到 [LINE Developers Console](https://developers.line.biz/console/) → 你的 Messaging API channel:

| 要的值 | 在哪拿 | 填到 `.env` 的 |
| --- | --- | --- |
| Channel access token | **Messaging API** 分頁最下方 long-lived token → **Issue** → Copy | `LINE_CHANNEL_ACCESS_TOKEN` |
| 你的 user ID | **Basic settings** 分頁最下方 **Your user ID**(`U` 開頭 33 碼) | `LINE_NOTIFY_TO` |

- token 要選 **long-lived**(另一種短效 token 15 分鐘就失效)。
- `Your user ID` 是你個人的 LINE user ID,**不是**官方帳號的 ID;推播對象就是它。
- Basic settings 分頁的 **Channel secret 不需要**(那是 webhook 驗簽用,本系統用不到)。

## Step 3 · 加官方帳號好友

要收得到推播,你本人必須先加這個官方帳號為好友:

- LINE Developers Console → **Messaging API** 分頁最上方有 QR code,用手機 LINE 掃描加入。

## Step 4 · 填入 .env 並重啟

```bash
LINE_CHANNEL_ACCESS_TOKEN=你的 long-lived channel access token
LINE_NOTIFY_TO=你的 user ID (U 開頭 33 碼)
```

- 本機:`docker compose up -d app`(或重跑 `go run ./cmd/server`)。
- 正式機 (走 CI/CD 部署):`.env` 的單一來源是 GitHub 的 `DEPLOY_ENV_FILE` secret,
  把上面兩行加進該 secret 內容後重跑部署即可 (見 [cicd-k8s.md](cicd-k8s.md))。

啟動成功時,LINE 會收到「📢 SYSTEM」啟動推播;之後每筆買賣成交都會收到
「標題 + 股票 / 市況 / 成交價 / 股數 / 金額 / 剩餘現金 + 交易理由」的多行文字通知
(與 Discord embed 內容一致,同一則通知兩邊同步發送)。

## 疑難排解

| 症狀 | 原因與解法 |
| --- | --- |
| log 出現 `LINE 通知未啟用` | `.env` 兩個變數沒填齊 (只填 token 沒填 user ID 也算),或容器沒吃到新 `.env` |
| log 出現 `LINE API 回應 401` | token 錯誤或已 Revoke,到 Console 重新 Issue 一次 |
| log 出現 `LINE API 回應 400` | `LINE_NOTIFY_TO` 不是合法 user ID (要 `U` 開頭 33 碼,不是官方帳號 ID) |
| API 回應 200 但手機沒收到 | 你還沒加該官方帳號好友,或把它封鎖了 (Step 3) |
| 通知失敗但交易正常 | 屬預期行為:通知失敗僅記 log,不影響成交與帳本 |

## 免費額度提醒

Messaging API 免費方案每月有推播訊息額度 (目前 200 則/月)。本系統一天最多
「啟動通知 + 少數幾筆成交」,遠低於額度,正常使用不會超額。
