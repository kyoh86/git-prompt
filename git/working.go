package git

import "bytes"

var trueBytes = []byte("true")

// IsWorking will check the current directory is inside work tree.
func IsWorking() (bool, error) {
	output, err := runGit(nil, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	return bytes.Equal(output, trueBytes), nil
}
