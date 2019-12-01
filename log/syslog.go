// +build !windows

package log

import (
	"io"
	"log/syslog"
)

func logger() io.Writer {
	l, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_USER, "git-prompt")
	if err != nil {
		panic(err)
	}
	return l
}
