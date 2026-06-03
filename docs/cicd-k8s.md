# CI/CD + k8s 部署

> 本地用 docker compose 部署可以跳過此文件。本節是把 image 推上 registry、由 k8s manifests repo 拉下來部署的進階流程。

## 設定與 CD pipeline 串接的 GitHub Actions Secret

需要在 GitHub repo Settings → Secrets → Actions 設定：

| Secret | 說明 |
|--------|------|
| `MANIFEST_REPO_PAT` | 有 manifests repo push 權限的 Personal Access Token |

`GITHUB_TOKEN` 自動提供，不需手動設定。

### 如何建立 MANIFEST_REPO_PAT

1. 前往 GitHub → 右上角頭像 → **Settings** → 左側最下方 **Developer settings**
2. 選擇 **Personal access tokens** → **Fine-grained tokens** → **Generate new token**
3. 設定：
   - **Token name**：`manifest-repo-ci`（自訂）
   - **Expiration**：依需求選擇
   - **Repository access**：選 **Only select repositories** → 選擇 `stockbot-long-backend-k8s-manifests`
   - **Permissions** → **Repository permissions** → **Contents**：設為 **Read and write**
4. 點擊 **Generate token**，複製產生的 token
5. 前往 `stockbot-long-backend` repo → **Settings** → **Secrets and variables** → **Actions** → **New repository secret**
   - **Name**：`MANIFEST_REPO_PAT`
   - **Secret**：貼上剛才複製的 token

---

## 匯入本 App 環境變數到 K8s

App 本身所需的環境變數（.env）需要匯入為 K8s Secret，供 Pod 使用。

### 步驟

1. 切換到目標 `.env` 的所在目錄
2. 執行以下指令：

```bash
kubectl -n myapp create secret generic myapp-env \
  --from-env-file=.env \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 驗證

```bash
kubectl -n myapp get secret myapp-env -o yaml   # 確認 Secret 已建立
kubectl -n myapp rollout restart deployment myapp  # 重啟 Pod 以套用新的環境變數
```

### 更新

修改 `.env` 後重新執行上面的 `kubectl create secret` 指令即可，`--dry-run=client -o yaml | kubectl apply -f -` 會自動覆蓋舊值。
