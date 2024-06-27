package logs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

type MyFormatter struct{}

func (m *MyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	timestamp := entry.Time.Format("2006-01-02 15:04:05")

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

	//HasCaller()為true才會有調用信息
	if entry.HasCaller() {
		fName := filepath.Base(entry.Caller.File)
		newLog = fmt.Sprintf("[%s][%s][%s:%d] %s\n",
			logLevel, timestamp, fName, entry.Caller.Line, entry.Message)
	} else {
		newLog = fmt.Sprintf("[%s][%s] %s\n", logLevel, timestamp, entry.Message)
	}

	b.WriteString(newLog)
	return b.Bytes(), nil
}

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
