package logging

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// logger_test.go 驗證自訂 logrus formatter 與 InitLogger。

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
