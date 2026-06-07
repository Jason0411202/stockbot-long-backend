# CI/CD 與 Kubernetes

本專案的 GitHub Actions 會在 PR 與 main 分支 push 時執行測試；main push 測試通過後會 build Docker image 並推送到 GHCR，再更新 manifests repo 的 image tag。

## Pipeline

| Job | 觸發 | 工作 |
| --- | --- | --- |
| `test` | PR 與 main push | `go vet ./...`、`go test ./... -v` |
| `build` | main push | build image 並推送 `sha` 與 `latest` tag |
| `deploy` | main push | 更新 `stockbot-long-backend-k8s-manifests` 的 `values.yaml` |

## 必要 Secret

| Secret | 說明 |
| --- | --- |
| `MANIFEST_REPO_PAT` | 可寫入 manifests repo 的 Personal Access Token |

`GITHUB_TOKEN` 可推送 GHCR package，但無法直接寫入另一個 manifests repo，因此需要 `MANIFEST_REPO_PAT`。

## 建立 `MANIFEST_REPO_PAT`

1. 到 GitHub 個人設定的 Developer settings。
2. 建立 Fine-grained personal access token。
3. Repository access 選擇 `stockbot-long-backend-k8s-manifests`。
4. Repository permissions 的 `Contents` 設為 Read and write。
5. 將 token 加到 backend repo 的 Actions secret，名稱為 `MANIFEST_REPO_PAT`。

## 匯入 `.env` 到 Kubernetes Secret

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

## 檢查

```bash
kubectl -n myapp get secret myapp-env
kubectl -n myapp rollout status deployment myapp
kubectl -n myapp logs deploy/myapp
```
