package app_context

import (
	"database/sql"
	"github.com/Jason0411202/stockbot-long-backend/internal/config"
	"github.com/Jason0411202/stockbot-long-backend/internal/logging"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

type AppContext struct {
	Db  *sql.DB
	Log *logrus.Logger
	Dg  *discordgo.Session
	Cfg *config.Config
}

// configPath 會優先使用環境變數 CONFIG_PATH，否則使用預設路徑 "config.yaml"。
func configPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "config.yaml"
}

func NewAppContext() *AppContext {
	log := logging.InitLogger()

	cfg, err := config.Load(configPath())
	if err != nil {
		log.Fatalf("載入 config.yaml 失敗: %v", err)
	}

	var db *sql.DB
	var dg *discordgo.Session

	return &AppContext{
		Db:  db,
		Log: log,
		Dg:  dg,
		Cfg: cfg,
	}
}
