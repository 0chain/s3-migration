package zlogger

import (
	"os"

	"github.com/0chain/gosdk/core/logger"
)

var defaultLogLevel = logger.DEBUG
var Logger logger.Logger

func init() {
	Logger.Init(defaultLogLevel, "0fs")
}

func SetLogFile(logFile string, verbose bool) {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	Logger.SetLogFile(f, verbose)
}
