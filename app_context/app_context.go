package app_context

import (
	"database/sql"
	"main/logs"

	"github.com/sirupsen/logrus"
)

type AppContext struct {
	Db  *sql.DB
	Log *logrus.Logger
}

func NewAppContext() *AppContext {
	log := logs.InitLogger()
	var db *sql.DB

	return &AppContext{
		Db:  db,
		Log: log,
	}
}
