package log

import (
	"context"
	"log"
	"log/syslog"
	"os"

	"github.com/wacul/ulog"
	"github.com/wacul/ulog/adapter/stdlog"
)

// Background will get context for ulog from context.Background().
func Background(verbose []bool) context.Context {
	return Context(context.Background(), verbose)
}

// Context will get context for ulog.
func Context(ctx context.Context, verbose []bool) context.Context {
	var level ulog.Level
	switch len(verbose) {
	case 0:
		l, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_USER, "git-prompt")
		if err != nil {
			panic(err)
		}

		level = ulog.WarnLevel
		log.SetOutput(l)
	case 1:
		level = ulog.InfoLevel
		log.SetOutput(os.Stderr)
	default:
		level = ulog.DebugLevel
		log.SetOutput(os.Stderr)
	}

	return ulog.Logger(ctx).WithAdapter(&stdlog.Adapter{Level: level})
}
