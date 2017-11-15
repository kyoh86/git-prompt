package git

import (
	"bufio"
	"bytes"
)

func scan(buf []byte) *bufio.Scanner {
	return bufio.NewScanner(bytes.NewReader(buf))
}

func str(buf []byte, err error) (string, error) {
	return string(bytes.TrimSpace(buf)), err
}

func count(buf []byte, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	count := 0
	diffLines := scan(buf)
	for diffLines.Scan() {
		count++
	}
	return count, nil
}
