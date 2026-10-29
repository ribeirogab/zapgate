package live

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type slogLogger struct {
	log    *slog.Logger
	module string
}

func newLogger(log *slog.Logger, module string) waLog.Logger {
	return &slogLogger{log: log, module: module}
}

func (l *slogLogger) Warnf(msg string, args ...any)  { l.write(slog.LevelWarn, msg, args) }
func (l *slogLogger) Errorf(msg string, args ...any) { l.write(slog.LevelError, msg, args) }
func (l *slogLogger) Infof(msg string, args ...any)  { l.write(slog.LevelInfo, msg, args) }
func (l *slogLogger) Debugf(msg string, args ...any) { l.write(slog.LevelDebug, msg, args) }

func (l *slogLogger) Sub(module string) waLog.Logger {
	return &slogLogger{log: l.log, module: l.module + "/" + module}
}

func (l *slogLogger) write(level slog.Level, msg string, args []any) {
	l.log.Log(context.Background(), level, fmt.Sprintf(msg, args...), "module", l.module)
}
