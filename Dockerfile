# Dockerfile 建立可部署的 stockbot-long-backend server image。

# ─── Build Stage ───
FROM golang:1.22-alpine AS builder
WORKDIR /app

# 先複製 go.mod/go.sum 並下載依賴，讓 Docker layer cache 可重用依賴層。
COPY go.mod go.sum ./
RUN go mod download

# 複製所有程式碼並編譯正式 server 入口。
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# ─── Runtime Stage ───
FROM alpine:3.19

# 安裝 HTTPS 與時區資料，供 TWSE API 與 time.LoadLocation 使用。
RUN apk add --no-cache ca-certificates tzdata

# 複製靜態 binary 到 runtime image。
WORKDIR /app
COPY --from=builder /app/server .

# 對外提供 Echo HTTP server port。
EXPOSE 8080
ENTRYPOINT ["./server"]
