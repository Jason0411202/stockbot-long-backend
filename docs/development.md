# 開發指南

本文件說明本機開發、測試與常用 CLI。正式部署請看 [deployment.md](deployment.md)。

## 前置需求

- Go 1.21.4 或相容版本。
- Docker Engine 24+ 或 Docker Desktop。
- 可使用 PowerShell、Git Bash 或一般 shell。

## 本機環境

```bash
cp .env.example .env
go mod download
```

若要完整跑 HTTP server 與 DB，使用 Docker Compose：

```bash
docker compose up -d --build
```

若只做離線回測，可先抓 CSV，不一定需要 MariaDB：

```bash
go run ./cmd/fetch_data
go run ./cmd/eval_csv
```

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `go test ./...` | 執行全專案測試 |
| `go run ./cmd/server` | 啟動正式 server 與交易 loop |
| `go run ./cmd/fetch_data` | 下載追蹤標的歷史資料到 `data/*.csv` |
| `go run ./cmd/eval_csv` | 使用 CSV 執行全期與 walk-forward 評估 |
| `go run ./cmd/evaluate` | 使用 DB 資料執行 walk-forward 評估 |
| `go run ./cmd/research_run` | 使用 DB 資料跑單一研究回測 |
| `go run ./cmd/db_probe` | 檢查 DB schema、table 與資料筆數 |

## 程式碼規範

- 新增檔案時，檔案開頭需說明該檔案責任。
- 新增函式、型別、常數與重要變數時，宣告前需說明用途。
- 函式內每個以空白分隔的主要程式區塊，需用註解描述「這段在做什麼」。
- 註解描述目前行為與功能，不寫「修 bug」、「暫時處理」等歷史脈絡。
- 交易引擎維持純記憶體、無 I/O；DB、Discord 與 HTTP 副作用留在外層 service 或 executor。
- 修改策略參數前，需同時跑全期、walk-forward 與 OOS 驗證。

## 測試策略

交易決策、回測指標、repository 與 service 都有單元測試。修改共用邏輯後至少執行：

```bash
go test ./...
```

若修改策略參數或資料載入流程，另需重跑：

```bash
go run ./cmd/fetch_data
go run ./cmd/eval_csv
```

## 資料與產物

`data/*.csv` 是本機回測快取。`bin/*.exe`、`*.exe`、Docker volume 與 `.env` 不應作為一般程式碼變更的一部分提交。
