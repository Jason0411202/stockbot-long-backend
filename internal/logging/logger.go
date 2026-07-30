// internal/logging/logger.go 建立專案共用的 logrus logger,依環境變數切換輸出格式與等級。
package logging

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// 環境變數鍵名:LOG_FORMAT 控制輸出格式 (json / text),LOG_LEVEL 控制最低輸出等級,
// APP_COMMIT 為 build 時注入的 git commit (log 的 version 欄位)。
const (
	envLogFormat = "LOG_FORMAT"
	envLogLevel  = "LOG_LEVEL"
	envAppCommit = "APP_COMMIT"
)

// MyFormatter 將 logrus entry 格式化成固定的可讀文字輸出 (本機開發用,帶 ANSI 顏色)。
type MyFormatter struct{}

// Format 將單筆 log entry 轉成包含時間、層級、訊息與結構化欄位的 byte slice。
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
		newLog = fmt.Sprintf("[%s][%s][%s:%d] %s%s\n",
			logLevel, timestamp, fName, entry.Caller.Line, entry.Message, formatFields(entry.Data))
	} else {
		newLog = fmt.Sprintf("[%s][%s] %s%s\n", logLevel, timestamp, entry.Message, formatFields(entry.Data))
	}

	// 將格式化後的訊息寫入 buffer 並回傳。
	b.WriteString(newLog)
	return b.Bytes(), nil
}

// formatFields 將結構化欄位 (WithFields) 以 " key=value" 形式附加在文字訊息後方。
func formatFields(data logrus.Fields) string {
	// 無欄位時回傳空字串,維持純訊息輸出。
	if len(data) == 0 {
		return ""
	}

	// 依欄位逐一串接 key=value (順序交由 map 走訪,文字模式僅供人眼閱讀)。
	var sb strings.Builder
	for k, v := range data {
		sb.WriteString(fmt.Sprintf(" %s=%v", k, v))
	}
	return sb.String()
}

// parseLevel 將 LOG_LEVEL 環境變數字串解析為 logrus 等級;空值或不合法值回退 Info。
func parseLevel(raw string) logrus.Level {
	// 空值直接採用預設 Info 等級。
	if raw == "" {
		return logrus.InfoLevel
	}

	// 交由 logrus 解析 (支援 trace/debug/info/warn/error/fatal/panic);失敗時回退 Info。
	lv, err := logrus.ParseLevel(strings.ToLower(raw))
	if err != nil {
		return logrus.InfoLevel
	}
	return lv
}

// newFormatter 依 LOG_FORMAT 環境變數選擇 formatter:
// "json" 回傳結構化 JSON (供 VictoriaLogs / Elasticsearch 等 log 系統做欄位查詢,無 ANSI 碼),
// 其餘值回傳彩色文字 (本機開發終端)。
func newFormatter(format string) logrus.Formatter {
	// json 模式:欄位名稱固定為 timestamp / level / message / caller,時間採 RFC3339。
	if strings.EqualFold(format, "json") {
		return &logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime: "timestamp",
				logrus.FieldKeyMsg:  "message",
			},
			CallerPrettyfier: shortCaller,
		}
	}

	// 預設回傳本機開發用的彩色文字 formatter。
	return &MyFormatter{}
}

// shortCaller 將 caller 縮短為「repo 相對路徑:行號」的 file 欄位與「套件.函式」的 func 欄位,
// 供 log 直接定位出處 (例 file="internal/service/trading_service.go:579"、
// func="service.(*tradingExecutor).logTrade"),同時避免洩漏編譯機的絕對路徑。
func shortCaller(f *runtime.Frame) (function string, file string) {
	// 函式名只保留最後一段 (捨棄完整 module 路徑前綴)。
	fn := f.Function
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		fn = fn[i+1:]
	}

	// 檔案路徑只保留最後三段 (涵蓋 internal/<套件>/<檔名> 的層級)。
	parts := strings.Split(filepath.ToSlash(f.File), "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return fn, fmt.Sprintf("%s:%d", strings.Join(parts, "/"), f.Line)
}

// globalFieldsHook 將整個 process 共用的欄位 (boot_id / version) 附掛到每一筆 log entry,
// 供依「哪一次啟動 / 哪個部署版本」切分 log;entry 已有同名欄位時不覆蓋。
type globalFieldsHook struct {
	fields logrus.Fields
}

// Levels 回報 hook 套用到所有 log 等級。
func (h *globalFieldsHook) Levels() []logrus.Level { return logrus.AllLevels }

// Fire 將共用欄位補進單筆 entry 的結構化欄位。
func (h *globalFieldsHook) Fire(e *logrus.Entry) error {
	for k, v := range h.fields {
		if _, ok := e.Data[k]; !ok {
			e.Data[k] = v
		}
	}
	return nil
}

// newBootID 產生本次啟動的短隨機識別碼 (8 碼 hex),同一 process 的所有 log 共用。
func newBootID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}

// InitLogger 建立並回傳專案預設 logger:
// 輸出格式由 LOG_FORMAT 決定 (json / text,預設 text)、最低等級由 LOG_LEVEL 決定 (預設 info)、
// 一律輸出至 stderr 並附帶呼叫者檔案路徑行號與函式名;
// JSON 模式 (生產) 另掛 boot_id (本次啟動識別碼) 與 version (APP_COMMIT) 兩個全域欄位。
func InitLogger() *logrus.Logger {
	// 創建一個新的 logrus 實例
	logger := logrus.New()

	// 依環境變數設定輸出格式與最低輸出等級。
	format := os.Getenv(envLogFormat)
	logger.SetFormatter(newFormatter(format))
	logger.SetLevel(parseLevel(os.Getenv(envLogLevel)))

	// 設定 logrus 輸出位置為 os.Stderr (終端輸出)
	logger.SetOutput(os.Stderr)

	// 設定報告呼叫函式的行數
	logger.SetReportCaller(true)

	// JSON 模式掛上全域欄位:boot_id 供依啟動切分、version 供對照部署版本 (未注入時為 dev)。
	if strings.EqualFold(format, "json") {
		version := os.Getenv(envAppCommit)
		if version == "" {
			version = "dev"
		}
		logger.AddHook(&globalFieldsHook{fields: logrus.Fields{
			"boot_id": newBootID(),
			"version": version,
		}})
	}

	return logger
}
