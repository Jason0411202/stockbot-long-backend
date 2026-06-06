package app_context

import (
	"os"
	"path/filepath"
	"testing"
)

// app_context_test.go 驗證設定路徑解析與 AppContext 組裝。

func TestConfigPath(t *testing.T) {
	// 預設值。
	t.Setenv("CONFIG_PATH", "")
	if p := configPath(); p != "config.yaml" {
		t.Fatalf("default configPath = %q, want config.yaml", p)
	}
	// 環境變數覆寫。
	t.Setenv("CONFIG_PATH", "/tmp/custom.yaml")
	if p := configPath(); p != "/tmp/custom.yaml" {
		t.Fatalf("env configPath = %q, want /tmp/custom.yaml", p)
	}
}

func TestNewAppContext_LoadsConfig(t *testing.T) {
	// Arrange — 指向一份最小有效設定。
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
track_stocks:
  - "00631L"
initial_cash: 100000
init_db_back_months: 60
back_testing_months: -1
buy_frac_basis: cash
bull_buy_frac: 0.20
bear_buy_frac: 0.02
sell_frac_of_position: 0.33
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CONFIG_PATH", path)

	// Act
	appCtx := NewAppContext()

	// Assert — Cfg 載入成功、Log 就緒、Db/Dg 尚未連線。
	if appCtx.Cfg == nil || len(appCtx.Cfg.TrackStocks) != 1 || appCtx.Cfg.TrackStocks[0] != "00631L" {
		t.Fatalf("config not loaded: %+v", appCtx.Cfg)
	}
	if appCtx.Log == nil {
		t.Fatalf("logger not initialized")
	}
	if appCtx.Db != nil || appCtx.Dg != nil {
		t.Fatalf("Db/Dg should be nil until connected")
	}
}
