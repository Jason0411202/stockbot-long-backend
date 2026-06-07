// internal/config/path.go 統一決定設定檔路徑，支援環境變數覆寫。
package config

import "os"

// Path 回傳設定檔路徑:優先使用環境變數 CONFIG_PATH,否則使用預設 "config.yaml"。
//
// 它移植自舊的 app_context.configPath(),讓所有 composition root (cmd/*) 以一致的方式
// 解析設定檔路徑,而不需依賴 app_context。注意 "config.yaml" 為相對路徑 (CWD 相依),
// binary 須由 repo root 執行 (或透過 CONFIG_PATH 指定絕對路徑)。
func Path() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "config.yaml"
}
