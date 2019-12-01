// +build windows

package log

import (
	"io"
	"os"
)

func logger() io.Writer {
	return os.Stderr
}
