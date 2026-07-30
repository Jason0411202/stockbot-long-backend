# LINE Bot 設定懶人包

照著 4 步做完,交易通知就會推播到 LINE。LINE 通知為**可選功能**:
`.env` 的 `LINE_CHANNEL_ACCESS_TOKEN` 留空即代表不使用,系統照常運作。

本系統使用 Messaging API 的 **broadcast(群發)**:啟動通知與每筆買賣成交,
會發給**所有加這個官方帳號好友的人**——想收通知的人掃 QR code 加好友即可,不用逐一設定名單。
**不接收訊息、不需要 webhook、也用不到 channel secret**,設定比一般 LINE bot 簡單很多。

## Step 1 · 建官方帳號、開 Messaging API

1. 到 [LINE Official Account Manager](https://manager.line.biz/) 用 LINE 帳號登入 → **建立帳號**(分類隨意選即可)。
2. 進該帳號 → **設定** → **Messaging API** → **啟用 Messaging API** → 建立或選一個 Provider。
3. (建議) **設定** → **回應設定**:自動回應訊息改為**停用**,避免加好友的人收到罐頭訊息。
   Webhook 不用開 —— 本系統只群發、不接收。

## Step 2 · 抄一個值

到 [LINE Developers Console](https://developers.line.biz/console/) → 你的 Messaging API channel:

| 要的值 | 在哪拿 | 填到 `.env` 的 |
| --- | --- | --- |
| Channel access token | **Messaging API** 分頁最下方 long-lived token → **Issue** → Copy | `LINE_CHANNEL_ACCESS_TOKEN` |

- token 要選 **long-lived**(另一種短效 token 15 分鐘就失效)。
- Basic settings 分頁的 **Channel secret 不需要**(那是 webhook 驗簽用,本系統用不到)。

## Step 3 · 想收通知的人加好友

broadcast 只會發給官方帳號的**好友**:

- LINE Developers Console → **Messaging API** 分頁最上方有 QR code,把它分享給想收通知的人
  (包含你自己),用手機 LINE 掃描加入即可。之後任何人加好友,自動開始收到通知。

## Step 4 · 填入 .env 並重啟

```bash
LINE_CHANNEL_ACCESS_TOKEN=你的 long-lived channel access token
```

- 本機:`docker compose up -d app`(或重跑 `go run ./cmd/server`)。
- 正式機 (走 CI/CD 部署):`.env` 的單一來源是 GitHub 的 `DEPLOY_ENV_FILE` secret,
  把上面這行加進該 secret 內容後重跑部署即可 (見 [cicd-k8s.md](cicd-k8s.md))。

啟動成功時,所有好友會收到「📢 SYSTEM」啟動群發;之後每筆買賣成交都會收到
「標題 + 股票 / 市況 / 成交價 / 股數 / 金額 / 剩餘現金 + 交易理由」的多行文字通知
(與 Discord embed 內容一致,同一則通知兩邊同步發送)。

## 疑難排解

| 症狀 | 原因與解法 |
| --- | --- |
| log 出現 `LINE 通知未啟用` | `.env` 沒填 `LINE_CHANNEL_ACCESS_TOKEN`,或容器沒吃到新 `.env` (重啟 app 容器) |
| log 出現 `LINE API 回應 401` | token 錯誤或已 Revoke,到 Console 重新 Issue 一次 |
| API 成功但手機沒收到 | 你還沒加該官方帳號好友,或把它封鎖了 (Step 3) |
| 通知失敗但交易正常 | 屬預期行為:通知失敗僅記 log,不影響成交與帳本 |

## 免費額度提醒

Messaging API 免費方案每月有訊息額度 (目前 200 則/月),**broadcast 依「實際送達人數」計算**:
1 則群發 × N 個好友 = N 則。本系統一天最多「啟動通知 + 少數幾筆成交」,
好友人數少時遠低於額度;若好友多,請自行估算 (每月訊息數 ≈ 發送次數 × 好友數)。
