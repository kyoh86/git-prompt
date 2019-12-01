// +build windows

import (
	"io"
	"os"
)

func logger() io.Writer {
	return os.Stderr
}
