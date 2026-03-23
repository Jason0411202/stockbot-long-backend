# ─── Build Stage ───
FROM golang:1.22-alpine AS builder
WORKDIR /app

# 先複製 go.mod/go.sum 並下載依賴
# Docker layer cache：依賴沒變就不重新下載，大幅加速 build
COPY go.mod go.sum ./
RUN go mod download

# 複製所有程式碼並編譯
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server main.go
# CGO_ENABLED=0：純靜態 binary，不依賴 shared library
# -ldflags="-s -w"：移除 debug 資訊，縮小 binary 大小

# ─── Runtime Stage ───
FROM alpine:3.19
# 全新的 Alpine（~5MB），不帶 Go 工具鏈（~300MB）

RUN apk add --no-cache ca-certificates tzdata
# ca-certificates：HTTPS 連線需要（呼叫 TWSE API）
# tzdata：Go 的 time.LoadLocation() 需要

WORKDIR /app
COPY --from=builder /app/server .
# 只從 builder 複製編譯好的 binary

# SQLcommend.sql 在 runtime 會被 os.ReadFile("./sqls/SQLcommend.sql") 讀取
COPY sqls/SQLcommend.sql ./sqls/SQLcommend.sql

EXPOSE 8080
ENTRYPOINT ["./server"]
