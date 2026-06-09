# CI/CD 與 Kubernetes

本專案的 GitHub Actions 在 PR 與 main push 時跑測試；main push 測試通過後 build Docker image 推到 GHCR，再依「開關」決定要不要部署。所有環境專屬資訊都走 GitHub 的 Variables / Secrets，**不寫死在 workflow 檔**，因此任何人 fork 後都能直接跑通、CD 也能自行設定，不需改任何程式碼。

## Pipeline

| Job | 觸發 | 開關 | 工作 |
| --- | --- | --- | --- |
| `test` | PR 與 main push | 無（一律執行） | `go vet ./...`、`go test ./... -v` |
| `build` | main push | 無（一律執行） | build image 並推 `sha` 與 `latest` tag 到 `ghcr.io/<owner>/<repo>` |
| `deploy-k8s` | main push | `vars.STOCKBOT_LONG_K8S_DEPLOY_ENABLED == 'true'` | 更新 manifest repo 的 image tag（觸發 ArgoCD） |
| `deploy-ssh` | main push | `vars.STOCKBOT_LONG_DEPLOY_ENABLED == 'true'` | SSH 到 server 跑 `docker compose pull && up -d` |

兩個 deploy job 各自獨立、互不影響，**未設對應開關時整個 job 自動 skip**（不會紅叉）。

> **命名慣例**：所有本專案的 Variables / Secrets 都以 `STOCKBOT_LONG_` 為前綴，避免與同帳號／組織下其他專案的 key 撞名。
>
> **開關旗標為選填、預設 false**：`STOCKBOT_LONG_K8S_DEPLOY_ENABLED` 與 `STOCKBOT_LONG_DEPLOY_ENABLED` 不設或非 `true` 一律視為關閉（job skip）；要部署才設為 `true`。本專案正式部署一律設為 `true` 啟用。

## Fork 後可以直接跑通嗎？

可以。fork 者只需到 **Actions 分頁手動啟用 workflows**（GitHub 對 fork 的預設限制，無法繞過），之後：

- `test`：零設定即可跑。
- `build`：用內建 `GITHUB_TOKEN` 推到 fork **自己的** GHCR（`ghcr.io/<fork-owner>/stockbot-long-backend`）。GHCR package 預設為 private，要讓 server 拉得到請把 package 設為 public，或在 server `docker login ghcr.io`。
- `deploy-k8s` / `deploy-ssh`：沒設開關（`STOCKBOT_LONG_*_DEPLOY_ENABLED`）→ 自動 skip → **CI 全綠**。想用哪條 CD，照下面填對應的 Variables / Secrets 即可，**完全不用改檔案**。

> `vars`（Variables）可在 job-level `if` 讀取，`secrets` 不行——所以「開關」一律用 Variable，機密值才放 Secret。

設定位置都在 **repo → Settings → Secrets and variables → Actions**。

## CD 路徑 A：Docker Compose（SSH 全自動部署，推薦給單機 server）

main 更新 → CI build 新 image → SSH 進 server **一次完成**「裝 Docker → 取得部署檔 → 寫 `.env` → 拉 image → 啟動」。

**全程自動，不需手動登入 server 做任何前置**：可直接對一台**全新、未裝過任何東西的 Linux server** 跑；對**已架好**的 server 則自動略過安裝、走滾動更新（`deploy-ssh` job 的腳本對兩種情況皆 idempotent）。部署目錄固定為登入帳號 home 下的 `~/stockbot-long-backend`、SSH port 固定 `22`。

### 自動化做了什麼

`deploy-ssh` 每次執行（對照 README 手動四步，全部自動化）：

1. **裝 Docker**：偵測 `docker` 是否存在，沒有就跑官方 `get.docker.com` 安裝（含 compose plugin）。
2. **取得部署檔**：到 `~/stockbot-long-backend`，從 repo main 下載最新 `docker-compose.yml`。
3. **寫 `.env`**：以 secret `STOCKBOT_LONG_ENV_FILE` 全文為單一來源，每次**覆蓋**寫入 `.env`。部署的 image **不寫進 `.env`**，而是在 `docker compose` 呼叫時以環境變數帶入——直接取用 `build` job 推出的不可變 `:<sha>` image（compose 代換 `${APP_IMAGE}` 時 shell 環境變數優先於 `.env`；fork 自動指向自己帳號）。
4. **拉 image 並啟動**：`docker compose pull && up -d`，並清掉舊 image。

### 要設定的 Variables

| 名稱 | 值 | 必填 |
| --- | --- | --- |
| `STOCKBOT_LONG_DEPLOY_ENABLED` | `true` | 選填（預設 false）；要啟用才設 |

### 要設定的 Secrets

| 名稱 | 說明 | 必填 |
| --- | --- | --- |
| `STOCKBOT_LONG_DEPLOY_HOST` | server IP 或網域 | 是 |
| `STOCKBOT_LONG_DEPLOY_USER` | SSH 登入帳號（部署目錄即此帳號的 `~/stockbot-long-backend`） | 是 |
| `STOCKBOT_LONG_DEPLOY_PASSWORD` | SSH 登入密碼 | 是 |
| `STOCKBOT_LONG_ENV_FILE` | 完整 `.env` 內容（正式機 DB 密碼、Discord、`SITE_ADDRESS` 等）。直接把整份 `.env` 貼進這個 secret；`APP_IMAGE` 不用填，部署時自動帶入 | 選填（不填則 `.env` 為空、compose 用內建預設值，僅適合測試） |

### 登入方式：密碼

一律以密碼登入：把登入密碼貼進 `STOCKBOT_LONG_DEPLOY_PASSWORD`，不需任何 SSH 金鑰。前提是 server 的 sshd 允許密碼登入（`PasswordAuthentication yes`；root 密碼登入另需 `PermitRootLogin yes`）。請用強密碼並搭配防火牆 / fail2ban。

### 關於特權（裝 Docker）

腳本所有特權命令一律加 `sudo` 前綴，因此 `STOCKBOT_LONG_DEPLOY_USER` **需能執行免密碼 sudo**（非互動 SSH 無法中途輸入 sudo 密碼）。root 帳號亦透過 sudo 執行，故 server 上需安裝 `sudo`。

> 為何「第一次進 server 的憑證」無法由 CI 生成：CI 必須先有登入密碼才連得進去，這密碼只能由開機器的人提供。改用密碼登入後，這件事只剩「把 server 帳密貼進 secret」——已是純 GitHub 參數，不再是 server 上的手動操作。

> 自架 fork：`APP_IMAGE` 不用設——部署時自動以 `github.repository` + 本次 commit `sha` 算出 `ghcr.io/<fork-owner>/stockbot-long-backend:<sha>`，於 `docker compose` 呼叫時以環境變數帶入，永遠指向你 fork 自己 build 的 image，compose 檔與 `.env` 都不用改。
>
> 注意：MariaDB 密碼在**第一次啟動**就寫進資料 volume；之後若改 `.env` 的 DB 密碼不會自動套用到既有 volume，需清掉 volume 重建（會刪資料）才一致。

## CD 路徑 B：Kubernetes（manifest repo + ArgoCD）

main 更新 → CI build 新 image → 更新另一個 manifests repo 的 `values.yaml` image tag → ArgoCD 偵測並同步。

### 要設定的 Variables

| 名稱 | 值 | 必填 |
| --- | --- | --- |
| `STOCKBOT_LONG_K8S_DEPLOY_ENABLED` | `true` | 選填（預設 false）；要啟用才設 |
| `STOCKBOT_LONG_MANIFEST_REPO` | manifests repo，如 `Jason0411202/stockbot-long-backend-k8s-manifests` | 啟用時必填 |

### 要設定的 Secrets

| 名稱 | 說明 |
| --- | --- |
| `STOCKBOT_LONG_MANIFEST_REPO_PAT` | 可寫入 `STOCKBOT_LONG_MANIFEST_REPO` 的 Personal Access Token |

`GITHUB_TOKEN` 只能推 GHCR package，無法寫入另一個 repo，因此 k8s 路徑需要 `STOCKBOT_LONG_MANIFEST_REPO_PAT`。

### 建立 `STOCKBOT_LONG_MANIFEST_REPO_PAT`

1. 到 GitHub 個人設定的 Developer settings。
2. 建立 Fine-grained personal access token。
3. Repository access 選擇你的 manifests repo。
4. Repository permissions 的 `Contents` 設為 Read and write。
5. 將 token 加到 backend repo 的 Actions secret，名稱為 `STOCKBOT_LONG_MANIFEST_REPO_PAT`。

### 匯入 `.env` 到 Kubernetes Secret

正式環境的 DB、Discord 與站台設定可由 `.env` 匯入 Kubernetes Secret：

```bash
kubectl -n myapp create secret generic myapp-env \
  --from-env-file=.env \
  --dry-run=client -o yaml | kubectl apply -f -
```

更新 secret 後重啟 deployment：

```bash
kubectl -n myapp rollout restart deployment myapp
```

### 檢查

```bash
kubectl -n myapp get secret myapp-env
kubectl -n myapp rollout status deployment myapp
kubectl -n myapp logs deploy/myapp
```
