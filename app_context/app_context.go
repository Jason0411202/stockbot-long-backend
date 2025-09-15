package app_context

import (
	"database/sql"
	"main/logs"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

type AppContext struct {
	Db  *sql.DB
	Log *logrus.Logger
	Dg  *discordgo.Session
}

func NewAppContext() *AppContext {
	log := logs.InitLogger()
	var db *sql.DB
	var dg *discordgo.Session

	return &AppContext{
		Db:  db,
		Log: log,
		Dg:  dg,
	}
}
