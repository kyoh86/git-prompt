package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	branch := os.Args[1]

	if !gitWorking() {
		os.Exit(1)
	}

	if gitUntrack() {
		fmt.Println("hook_com[untracked]=\"true\"")
	}

	fmt.Printf("hook_com[email]=%q\n", gitEmail())
	fmt.Printf("hook_com[stash_count]=%d\n", gitStash())
	fmt.Printf("hook_com[upstream]=%q\n", gitUpstream())
	fmt.Printf("hook_com[ahead]=%d\n", gitAhead(branch))
	fmt.Printf("hook_com[behind]=%d\n", gitBehind(branch))
	lastEmail, lastMessage := gitLastCommit()
	fmt.Printf("hook_com[last_email]=%q\n", lastEmail)
	fmt.Printf("hook_com[last_message]=%q\n", lastMessage)
	baseBranch := gitBaseBranch(branch)
	fmt.Printf("hook_com[base_branch]=%q\n", baseBranch)
	fmt.Printf("hook_com[base_behind]=%d\n", gitBehindFrom(branch, baseBranch))
}

func git(args ...string) []byte {
	command := exec.Command("git", args...)
	output, _ := command.Output()
	// if err != nil {
	// 	os.Exit(-1)
	// }
	if output == nil {
		return []byte{}
	}
	return bytes.TrimSpace(output)
}

func scan(buf []byte) *bufio.Scanner {
	return bufio.NewScanner(bytes.NewReader(buf))
}

func count(buf []byte) int {
	count := 0
	diffLines := scan(buf)
	for diffLines.Scan() {
		count++
	}
	return count
}

func gitUpstream() string {
	return string(git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
}

func gitWorking() bool {
	return string(git("rev-parse", "--is-inside-work-tree")) == "true"
}

func gitUntrack() bool {
	untrackLines := scan(git("status", "--porcelain"))
	for untrackLines.Scan() {
		fields := strings.Fields(untrackLines.Text())
		if len(fields) > 0 && fields[0] == "??" {
			return true
		}
	}
	return false
}

func gitStash() int {
	return count(git("stash", "list"))
}

func gitDiff(baseBranch, headBranch string) int {
	return count(git("rev-list", baseBranch+".."+headBranch))
}

func gitAhead(branch string) int {
	return gitDiff(branch+"@{u}", branch)
}

func gitBehind(branch string) int {
	return gitBehindFrom(branch, branch+"@{u}")
}

func gitBehindFrom(branch, baseBranch string) int {
	return gitDiff(branch, baseBranch)
}

func gitBaseBranch(branch string) string {
	remoteLines := scan(git("branch", "-r"))
	maxMatched := 0
	branchBytes := []byte(branch)
	var baseBranchBytes []byte

	for remoteLines.Scan() {
		remoteBranch := bytes.TrimSpace(remoteLines.Bytes())
		remoteFields := bytes.SplitN(remoteBranch, []byte{'/'}, 2)
		if len(remoteFields) < 2 {
			continue
		}
		remoteLength := len(remoteFields[1])
		if maxMatched > remoteLength {
			continue
		}
		if bytes.HasPrefix(branchBytes, append(remoteFields[1], '-')) {
			maxMatched = remoteLength
			baseBranchBytes = remoteBranch
		}
	}

	if baseBranchBytes == nil {
		return "origin/master"
	}

	return string(baseBranchBytes)
}

func gitEmail() string {
	return string(git("config", "user.email"))
}

func gitLastCommit() (email, message string) {
	head := git("log", "-n1", "--pretty=%ce %s")
	fields := bytes.Fields(head)
	if len(fields) > 0 {
		email = string(fields[0])
	}
	if len(fields) > 1 {
		message = string(fields[1])
	}
	return
}
