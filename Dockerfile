# 使用 Golang 官方提供的最新 Golang 映像檔作為基底
FROM golang:latest

# 設定工作目錄
WORKDIR /usr/src/app

# 複製程式碼和相關文件到容器內的工作目錄
COPY go.mod .
COPY go.sum .

# 下載並安裝依賴套件
RUN go mod download

# 複製其他程式碼到容器內的工作目錄
COPY . .

# 設定這個容器對外開放 8000 port
EXPOSE 8000

# 執行 go run main.go 指令
CMD ["go", "run", "main.go"]