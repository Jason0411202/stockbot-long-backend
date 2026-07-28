// internal/logging/logger_test.go 驗證自訂 logger 格式器在各層級與 caller 下的輸出。
package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// logger_test.go 驗證自訂 logrus formatter 與 InitLogger。

// TestInitLogger_FormatsWithCaller 驗證 InitLogger 產生的 logger 輸出含訊息、等級及呼叫者檔名。
func TestInitLogger_FormatsWithCaller(t *testing.T) {
	// Arrange — InitLogger 開了 ReportCaller,故輸出應含檔名:行號 + 訊息 + 等級。
	logger := InitLogger()
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	// Act
	logger.Info("hello world")

	// Assert
	out := buf.String()
	if !strings.Contains(out, "hello world") || !strings.Contains(out, "INFO") {
		t.Fatalf("log output missing message/level: %q", out)
	}
	if !strings.Contains(out, "logger_test.go") {
		t.Fatalf("expected caller file in output: %q", out)
	}
}

// TestInitLogger_JSONFormat 驗證 LOG_FORMAT=json 時輸出為結構化 JSON 且欄位名正確、無 ANSI 碼。
func TestInitLogger_JSONFormat(t *testing.T) {
	// Arrange — 以環境變數切換 JSON 模式並攔截輸出。
	t.Setenv("LOG_FORMAT", "json")
	logger := InitLogger()
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	// Act
	logger.WithField("stock_id", "00631L").Info("json output")

	// Assert — 應為單行合法 JSON,含 timestamp/level/message/自訂欄位,且不含 ANSI 跳脫碼。
	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Fatalf("json output should not contain ANSI codes: %q", out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	for _, key := range []string{"timestamp", "level", "message", "stock_id"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("json output missing field %q: %q", key, out)
		}
	}
	if m["message"] != "json output" || m["stock_id"] != "00631L" {
		t.Fatalf("unexpected field values: %v", m)
	}
}

// TestInitLogger_LevelFromEnv 驗證 LOG_LEVEL 控制最低輸出等級,且不合法值回退 info。
func TestInitLogger_LevelFromEnv(t *testing.T) {
	// Arrange + Act + Assert — 各環境變數值對應的期望等級 (含空值與不合法值回退)。
	cases := []struct {
		raw  string
		want logrus.Level
	}{
		{"", logrus.InfoLevel},
		{"debug", logrus.DebugLevel},
		{"WARN", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"not-a-level", logrus.InfoLevel},
	}
	for _, tc := range cases {
		t.Setenv("LOG_LEVEL", tc.raw)
		if got := InitLogger().GetLevel(); got != tc.want {
			t.Fatalf("LOG_LEVEL=%q: level = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestMyFormatter_StructuredFields 驗證文字模式將 WithFields 欄位以 key=value 附加在訊息後。
func TestMyFormatter_StructuredFields(t *testing.T) {
	// Arrange
	f := &MyFormatter{}

	// Act
	out, err := f.Format(&logrus.Entry{
		Level:   logrus.InfoLevel,
		Message: "msg",
		Time:    time.Unix(0, 0),
		Data:    logrus.Fields{"stock_id": "00830"},
	})

	// Assert — 欄位應以 key=value 形式出現在輸出中。
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(string(out), "stock_id=00830") {
		t.Fatalf("expected key=value field in output: %q", out)
	}
}

// TestMyFormatter_AllLevels 驗證 MyFormatter.Format 對每個 logrus 等級皆能正確格式化輸出。
func TestMyFormatter_AllLevels(t *testing.T) {
	// Arrange — 直接呼叫 Format (無 caller 分支),逐一覆蓋各等級 + default。
	f := &MyFormatter{}
	levels := []logrus.Level{
		logrus.DebugLevel, logrus.InfoLevel, logrus.WarnLevel,
		logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel, logrus.TraceLevel,
	}
	for _, lv := range levels {
		// Act
		out, err := f.Format(&logrus.Entry{Level: lv, Message: "msg", Time: time.Unix(0, 0)})
		// Assert
		if err != nil {
			t.Fatalf("Format(%v): %v", lv, err)
		}
		if !strings.Contains(string(out), "msg") {
			t.Fatalf("Format(%v) missing message: %q", lv, out)
		}
	}
}
