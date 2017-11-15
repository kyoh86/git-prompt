package git

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/errors"
)

// Git handles git command and get informations from repository.
type Git struct {
	dir     string
	tmpFile *os.File
	envs    []string

	cache sync.Map
}

// func OpenDir(dir string)

// OpenDir current directory
func OpenDir(dir string) (git *Git, reterr error) {
	git = &Git{}

	{
		output, err := runGit(func(cmd *exec.Cmd) {
			cmd.Dir = dir
		}, `rev-parse`, `--show-toplevel`)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open current directory")
		}
		git.dir = string(bytes.TrimSpace(output))
	}

	{
		tmpFile, err := ioutil.TempFile("", "git-prompt")
		if err != nil {
			return nil, errors.Wrap(err, "failed to create a tempfile")
		}
		git.tmpFile = tmpFile
	}
	{
		src, err := os.Open(filepath.Join(git.dir, ".git", "index"))
		if err != nil {
			return nil, errors.Wrap(err, "failed to open an index file")
		}
		defer func() {
			if err := src.Close(); err != nil && reterr == nil {
				reterr = err
			}
		}()
		buffer := make([]byte, 1024*1024)
		if _, err := io.CopyBuffer(git.tmpFile, src, buffer); err != nil {
			return nil, errors.Wrap(err, "failed to copy an index file")
		}
	}
	git.envs = append(os.Environ(), "GIT_INDEX_FILE="+git.tmpFile.Name())
	return git, nil
}

// Close git repository
func (g *Git) Close() error {
	if g.tmpFile == nil {
		return nil
	}
	if err := g.tmpFile.Close(); err != nil {
		return err
	}
	return os.Remove(g.tmpFile.Name())
}

// Call git with arguments. If true given in lock, escape the index file.
func (g *Git) Call(args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	if cache, ok := g.cache.Load(key); ok {
		return cache.([]byte), nil
	}
	output, err := runGit(func(cmd *exec.Cmd) {
		cmd.Env = g.envs
		cmd.Dir = g.dir
	}, args...)
	if err != nil {
		return nil, err
	}
	g.cache.Store(key, output)
	return output, nil
}

// Root directory
func (g *Git) Root() string {
	return g.dir
}

// CurrentBranch will get the current branch
func (g *Git) CurrentBranch() (string, error) {
	return str(g.Call("rev-parse", "--abbrev-ref", "--symbolic-full-name", "HEAD"))
}

// Upstream will get the upstream of a current branch
func (g *Git) Upstream() (string, error) {
	return strOrEmpty(g.Call("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
}

// Remote will get the remote of a branch
func (g *Git) Remote(branch string) (string, error) {
	return strOrEmpty(g.Call("config", "--local", "--get", "branch."+branch+".remote"))
}

// RemoteURL will get the url of a remote
func (g *Git) RemoteURL(remote string) (string, error) {
	if remote == "" {
		return "", nil
	}
	return str(g.Call("remote", "get-url", remote))
}

// Staged searches staged file
func (g *Git) Staged() (bool, error) {
	output, err := g.Call("status", "--porcelain")
	if err != nil {
		return false, err
	}
	untrackLines := scan(output)
	for untrackLines.Scan() {
		line := untrackLines.Text()
		if len(line) < 1 {
			continue
		}
		if line[0] == 'M' || line[0] == 'D' || line[0] == 'R' || line[0] == 'A' {
			return true, nil
		}
	}
	return false, nil
}

// Unstaged searches unstaged file
func (g *Git) Unstaged() (bool, error) {
	output, err := g.Call("status", "--porcelain")
	if err != nil {
		return false, err
	}
	untrackLines := scan(output)
	for untrackLines.Scan() {
		line := untrackLines.Text()
		if len(line) < 2 {
			continue
		}
		if line[1] == 'M' || line[1] == 'D' {
			return true, nil
		}
	}
	return false, nil
}

// Untracked searches untracked file
func (g *Git) Untracked() (bool, error) {
	output, err := g.Call("status", "--porcelain")
	if err != nil {
		return false, err
	}
	untrackLines := scan(output)
	for untrackLines.Scan() {
		fields := strings.Fields(untrackLines.Text())
		if len(fields) > 0 && fields[0] == "??" {
			return true, nil
		}
	}
	return false, nil
}

// StashCount counts stash list
func (g *Git) StashCount() (int, error) {
	return count(g.Call("stash", "list"))
}

// DiffCount counts rev-list between branches
func (g *Git) DiffCount(baseBranch, headBranch string) (int, error) {
	return countOrZero(g.Call("rev-list", baseBranch+".."+headBranch))
}

// AheadCount counts rev-list from upstream
func (g *Git) AheadCount(branch string) (int, error) {
	return g.DiffCount(branch+"@{u}", branch)
}

// BehindCount counts rev-list to upstream
func (g *Git) BehindCount(branch string) (int, error) {
	return g.BehindCountFrom(branch, branch+"@{u}")
}

// BehindCountFrom counts rev-list between branches
func (g *Git) BehindCountFrom(branch, baseBranch string) (int, error) {
	return g.DiffCount(branch, baseBranch)
}

// BaseBranch find a branch with longest match for.
func (g *Git) BaseBranch(branch string) (string, error) {
	output, err := g.Call("branch", "-r")
	if err != nil {
		return "", err
	}
	remoteLines := scan(output)
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
		if bytes.HasPrefix(branchBytes, append(remoteFields[1], '/')) {
			maxMatched = remoteLength
			baseBranchBytes = remoteBranch
		} else if bytes.HasPrefix(branchBytes, append(remoteFields[1], '-')) {
			maxMatched = remoteLength
			baseBranchBytes = remoteBranch
		}
	}

	if baseBranchBytes == nil {
		return "origin/master", nil
	}

	return string(baseBranchBytes), nil
}

// Email will get user account from git config.
func (g *Git) Email() (string, error) {
	return str(g.Call("config", "user.email"))
}

// LastCommit will get the last commit account, message and hash.
func (g *Git) LastCommit() (email, message, hash string, err error) {
	head, err := g.Call("log", "-n1", "--pretty=%ce %s %h")
	if err != nil {
		return "", "", "", err
	}
	fields := bytes.Fields(head)
	if len(fields) > 0 {
		email = string(fields[0])
	}
	if len(fields) > 1 {
		message = string(fields[1])
	}
	if len(fields) > 2 {
		hash = string(fields[2])
	}
	return
}

func runGit(mod func(*exec.Cmd), args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	if mod != nil {
		mod(command)
	}
	output, err := command.Output()
	if err != nil {
		all, _ := command.CombinedOutput()
		return nil, errors.Wrapf(err, "failed to run git (%q: %q)", strings.Join(args, " "), string(append(output, all...)))
	}
	return output, nil
}
