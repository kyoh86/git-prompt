package git

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/pkg/errors"
)

func scanFunc(buf []byte) func(*string) bool {
	s := scanner(buf)
	return func(v *string) bool {
		if !s.Scan() {
			return false
		}
		*v = s.Text()
		return true
	}
}
func scanner(buf []byte) *bufio.Scanner {
	return bufio.NewScanner(bytes.NewReader(buf))
}

func strOrEmpty(buf []byte, err error) (string, error) {
	if err != nil && strings.HasPrefix(errors.Cause(err).Error(), "exit status ") {
		err = nil
	}
	return str(buf, err)
}
func str(buf []byte, err error) (string, error) {
	return string(bytes.TrimSpace(buf)), err
}

func countOrZero(buf []byte, err error) (int, error) {
	if err != nil && strings.HasPrefix(errors.Cause(err).Error(), "exit status ") {
		err = nil
	}
	return count(buf, err)
}

func count(buf []byte, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	count := 0
	diffLines := scanner(buf)
	for diffLines.Scan() {
		count++
	}
	return count, nil
}
