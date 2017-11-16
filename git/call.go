package git

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var (
	// ErrIsNotInWorkingDirectory :
	ErrIsNotInWorkingDirectory = errors.New("not in working directory")
)

// OpenDir current directory
func OpenDir(dir string) (git *Git, reterr error) {
	git = &Git{}

	{
		output, _ := runGit(func(cmd *exec.Cmd) {
			cmd.Dir = dir
		}, `rev-parse`, `--is-inside-work-tree`)
		if !bytes.Equal([]byte(`true`), bytes.TrimSpace(output)) {
			return nil, ErrIsNotInWorkingDirectory
		}
	}

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

// BranchVar :
func (g *Git) BranchVar(v *string) error {
	return stringSetter(g.Branch())(v)
}

const (
	branchPrefix     = "## "
	branchInitPrefix = branchPrefix + "No commits yet on "
)

var (
	branchRegexp = regexp.MustCompile(`^## (\S+)\.\.\.(\S+/\S+)( \[(?:ahead|behind) \d+\])?$`)
)

// Branch :
func (g *Git) Branch() (string, error) {
	output, err := g.Call("status", "--branch", "--porcelain")
	if err != nil {
		return "", err
	}
	untrackLines := scanner(output)
	if !untrackLines.Scan() {
		return "", nil
	}
	line := untrackLines.Text()
	if !strings.HasPrefix(line, branchPrefix) {
		return "", nil
	}

	if strings.HasPrefix(line, branchInitPrefix) {
		return strings.TrimPrefix(line, branchInitPrefix), nil
	}
	if matches := branchRegexp.FindStringSubmatch(line); len(matches) >= 2 {
		return matches[1], nil
	}
	return strings.TrimPrefix(line, branchPrefix), nil
}

// UpstreamVar :
func (g *Git) UpstreamVar(v *string) error {
	return stringSetter(g.Upstream())(v)
}

// Upstream :
func (g *Git) Upstream() (string, error) {
	output, err := g.Call("status", "--branch", "--porcelain")
	if err != nil {
		return "", err
	}
	untrackLines := scanner(output)
	if !untrackLines.Scan() {
		return "", nil
	}
	line := untrackLines.Text()
	if matches := branchRegexp.FindStringSubmatch(line); len(matches) >= 3 {
		return matches[2], nil
	}
	return "", nil
}

// RemoteVar :
func (g *Git) RemoteVar(branch string, v *string) error {
	return stringSetter(g.Remote(branch))(v)
}

// Remote :
func (g *Git) Remote(branch string) (string, error) {
	return strOrEmpty(g.Call("config", "--local", "--get", "branch."+branch+".remote"))
}

// RemoteURLVar :
func (g *Git) RemoteURLVar(remote string, v *string) error {
	return stringSetter(g.RemoteURL(remote))(v)
}

// RemoteURL :
func (g *Git) RemoteURL(remote string) (string, error) {
	return strOrEmpty(g.Call("remote", "get-url", remote))
}

// StashCountVar :
func (g *Git) StashCountVar(v *int) error {
	return intSetter(g.StashCount())(v)
}

// StashCount :
func (g *Git) StashCount() (int, error) {
	return count(g.Call("stash", "list"))
}

func (g *Git) diffCount(baseBranch, headBranch string) (int, error) {
	return countOrZero(g.Call("rev-list", baseBranch+".."+headBranch))
}

// AheadCountVar :
func (g *Git) AheadCountVar(v *int) error {
	//HACK: get from status --porcelain
	return intSetter(g.AheadCount())(v)
}

// AheadCount :
func (g *Git) AheadCount() (int, error) {
	return g.diffCount(Head+"@{u}", Head)
}

// BehindCountVar :
func (g *Git) BehindCountVar(v *int) error {
	//HACK: get from status --porcelain
	return intSetter(g.BehindCount())(v)
}

// Head :
const Head = "HEAD"

// BehindCount :
func (g *Git) BehindCount() (int, error) {
	return g.BehindCountFrom(Head + "@{u}")
}

// BehindCountFromVar :
func (g *Git) BehindCountFromVar(baseBranch string, v *int) error {
	return intSetter(g.BehindCountFrom(baseBranch))(v)
}

// BehindCountFrom :
func (g *Git) BehindCountFrom(baseBranch string) (int, error) {
	return g.diffCount(Head, baseBranch)
}

// EmailVar :
func (g *Git) EmailVar(v *string) error {
	return stringSetter(g.Email())(v)
}

// Email :
func (g *Git) Email() (string, error) {
	return str(g.Call("config", "user.email"))
}

// LastCommitterVar :
func (g *Git) LastCommitterVar(v *string) error {
	return stringSetter(g.LastCommitter())(v)
}

// LastCommitter :
func (g *Git) LastCommitter() (string, error) {
	return str(g.Call("log", "-n1", "--pretty=%ce"))
}

// LastCommitMessageVar :
func (g *Git) LastCommitMessageVar(v *string) error {
	return stringSetter(g.LastCommitMessage())(v)
}

// LastCommitMessage :
func (g *Git) LastCommitMessage() (string, error) {
	return str(g.Call("log", "-n1", "--pretty=%s"))
}

// LastCommitHashVar :
func (g *Git) LastCommitHashVar(v *string) error {
	return stringSetter(g.LastCommitHash())(v)
}

// LastCommitHash :
func (g *Git) LastCommitHash() (string, error) {
	return str(g.Call("log", "-n1", "--pretty=%h"))
}

// StagedVar :
func (g *Git) StagedVar(v *bool) error {
	return boolSetter(g.Staged())(v)
}

// Staged :
func (g *Git) Staged() (bool, error) {
	output, err := g.Call("status", "--branch", "--porcelain")
	if err != nil {
		return false, err
	}
	var line string
	for lines := scanFunc(output); lines(&line); {
		if len(line) >= 1 && (line[0] == 'M' || line[0] == 'D' || line[0] == 'R' || line[0] == 'A') {
			return true, nil
		}
	}
	return false, nil
}

// UnstagedVar :
func (g *Git) UnstagedVar(v *bool) error {
	return boolSetter(g.Unstaged())(v)
}

// Unstaged :
func (g *Git) Unstaged() (bool, error) {
	output, err := g.Call("status", "--branch", "--porcelain")
	if err != nil {
		return false, err
	}
	var line string
	for lines := scanFunc(output); lines(&line); {
		if len(line) >= 2 && (line[1] == 'M' || line[1] == 'D') {
			return true, nil
		}
	}
	return false, nil
}

// UntrackedVar :
func (g *Git) UntrackedVar(v *bool) error {
	return boolSetter(g.Untracked())(v)
}

// Untracked :
func (g *Git) Untracked() (bool, error) {
	output, err := g.Call("status", "--branch", "--porcelain")
	if err != nil {
		return false, err
	}
	var line string
	for lines := scanFunc(output); lines(&line); {
		if strings.HasPrefix(line, "??") {
			return true, nil
		}
	}
	return false, nil
}

// BaseBranchVar :
func (g *Git) BaseBranchVar(branch string, v *string) error {
	return stringSetter(g.BaseBranch(branch))(v)
}

// BaseBranch :
func (g *Git) BaseBranch(branch string) (string, error) {
	output, err := g.Call("branch", "-r")
	if err != nil {
		return "", err
	}

	var maxMatched int
	var baseBranch string
	var line string
	for lines := scanFunc(output); lines(&line); {
		remoteFields := strings.SplitN(line, "/", 2) // 不正確: remote-nameやbranch-nameには/が使用できる
		if len(remoteFields) < 2 {
			continue
		}
		remoteLength := len(remoteFields[1])
		if maxMatched > remoteLength {
			continue
		}
		if strings.HasPrefix(branch, remoteFields[1]+"/") {
			maxMatched = remoteLength
			baseBranch = line
		} else if strings.HasPrefix(branch, remoteFields[1]+"-") {
			maxMatched = remoteLength
			baseBranch = line
		}
	}

	if baseBranch == "" {
		return "origin/master", nil
	}

	return baseBranch, nil
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
