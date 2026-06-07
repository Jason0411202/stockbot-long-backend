// internal/logging/logger.go 建立專案共用的 logrus logger 與輸出格式。
package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// MyFormatter 將 logrus entry 格式化成固定的可讀文字輸出。
type MyFormatter struct{}

// Format 將單筆 log entry 轉成包含時間、層級與訊息的 byte slice。
func (m *MyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	// 取用 entry 自帶的 buffer 或自行配置一個新的。
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	// 將時間格式化為可讀的字串。
	timestamp := entry.Time.Format("2006-01-02 15:04:05")

	// 依層級對應帶有 ANSI 顏色碼的標籤字串。
	var logLevel string
	switch entry.Level {
	case logrus.DebugLevel:
		logLevel = "\033[1;35mDEBUG\033[0m" // 使用紫色上色
	case logrus.InfoLevel:
		logLevel = "\033[1;32mINFO\033[0m" // 使用綠色上色
	case logrus.WarnLevel:
		logLevel = "\033[1;33mWARN\033[0m" // 使用黃色上色
	case logrus.ErrorLevel:
		logLevel = "\033[1;31mERROR\033[0m" // 使用紅色上色
	case logrus.FatalLevel:
		logLevel = "\033[1;31mFATAL\033[0m" // 使用紅色上色
	case logrus.PanicLevel:
		logLevel = "\033[1;31mPANIC\033[0m" // 使用紅色上色
	default:
		logLevel = fmt.Sprintf("[%s]", entry.Level)
	}

	var newLog string

	// HasCaller() 為 true 時在訊息前附加呼叫檔名與行號。
	if entry.HasCaller() {
		fName := filepath.Base(entry.Caller.File)
		newLog = fmt.Sprintf("[%s][%s][%s:%d] %s\n",
			logLevel, timestamp, fName, entry.Caller.Line, entry.Message)
	} else {
		newLog = fmt.Sprintf("[%s][%s] %s\n", logLevel, timestamp, entry.Message)
	}

	// 將格式化後的訊息寫入 buffer 並回傳。
	b.WriteString(newLog)
	return b.Bytes(), nil
}

// InitLogger 建立並回傳專案預設 logger。
func InitLogger() *logrus.Logger {
	// 創建一個新的 logrus 實例
	logger := logrus.New()

	// 設定 logrus 日誌紀錄格式
	logger.SetFormatter(&MyFormatter{})

	// 設定 logrus 輸出位置為 os.Stderr (終端輸出)
	logger.SetOutput(os.Stderr)

	// 設定報告呼叫函式的行數
	logger.SetReportCaller(true)

	return logger
}
