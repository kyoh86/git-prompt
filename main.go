package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kyoh86/xdg"
	"github.com/wacul/ptr"
	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/config"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

func assertError(err error, doing string, args ...interface{}) {
	if err != nil {
		log.Fatalf("failed to %s: %s", fmt.Sprintf(doing, args...), err.Error())
	}
}

// Stat holds git statuses
type Stat struct {
	Base        string
	Subdir      string
	Branch      string
	Revision    string
	Staged      bool
	Unstaged    bool
	Untracked   bool
	Email       string
	StashCount  int
	BaseName    string
	LastEmail   string
	LastMessage string
	Upstream    string
	Behind      int
	Ahead       int
	BaseBranch  string
	BaseBehind  int
}

func countCommit(rep *git.Repository, toCommit *object.Commit, fromCommit *object.Commit) (int, error) {
	toMap := map[plumbing.Hash]struct{}{
		toCommit.Hash: struct{}{},
	}
	toArr := []plumbing.Hash{}

	fromMap := map[plumbing.Hash]struct{}{
		fromCommit.Hash: struct{}{},
	}

	toIter := object.NewCommitPreorderIter(toCommit, nil)
	fromIter := object.NewCommitPreorderIter(fromCommit, nil)
	var commonCommit *plumbing.Hash
	for {
		remain := false
		toNext, err := toIter.Next()
		if err != nil && err != io.EOF {
			return 0, err
		}
		if toNext != nil {
			remain = true
			toMap[toNext.Hash] = struct{}{}
			if _, ok := fromMap[toNext.Hash]; ok {
				commonCommit = &toNext.Hash
				break
			}
			toArr = append(toArr, toNext.Hash)
		}

		fromNext, err := fromIter.Next()
		if err != nil && err != io.EOF {
			return 0, err
		}
		if fromNext != nil {
			remain = true
			fromMap[fromNext.Hash] = struct{}{}
			if _, ok := toMap[fromNext.Hash]; ok {
				commonCommit = &fromNext.Hash
				break
			}
		}
		if !remain {
			break
		}
	}
	if commonCommit == nil {
		return len(toArr), nil
	}
	for i, h := range toArr {
		if h == *commonCommit {
			return i, nil
		}
	}
	return len(toArr), nil
}

func main() {
	templates := map[string]string{
		"prompt": `%F{yellow}
	{{- if eq .Staged true -}}    + {{- end -}}
	{{- if eq .Unstaged true -}}  - {{- end -}}
	{{- if eq .Untracked true -}} ? {{- end -}}
	%f
	{{- if and (eq .LastMessage "wip") (eq .Email .LastEmail) -}}
		%F{red}!wip!%f
	{{- end -}}
	{{- if gt .Ahead 0 -}}  %F{red}⬆ {{.Ahead}}%f      {{- end -}}
	{{- if gt .Behind 0 -}} %F{magenta}⬇ {{.Behind}}%f {{- end -}}
	{{- if gt .BaseBehind 0 -}}
    %F{yellow}(.BaseBranch%f%F{red}-.BaseBehind%f%F{yellow})%f"
	{{- end -}}
	{{- if eq .Upstream "" -}}
    %F{red}%B⚠ %b%f
	{{- end -}}
	{{- if gt .StashCount 0 -}}
    %F{yellow}♻ {{.StashCount}}%f
	{{- end}} %F{blue}[{{.BaseName}}%f
	{{- if ne .Subdir "."}}
		%F{yellow}/{{.Subdir}}%f
	{{- end -}}
	{{- if and (ne .Branch "master") (ne .Branch "") -}}
		%F{green}:{{.Branch}}%f
	{{- end -}}
	%F{blue}]%f`,
	}

	var format = flag.String("f", "", "format for stats")
	var formatTmp = flag.String("t", "", "template of format for stats {prompt|status}")
	flag.Parse()

	if formatTmp != nil {
		format = ptr.String(templates[*formatTmp])
	}

	logger, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_USER, "git-prompt")
	if err != nil {
		panic(err)
	}
	log.SetOutput(logger)

	wd, err := os.Getwd()
	assertError(err, "get working directory")

	tmp, err := template.New("stat").Parse(*format)
	assertError(err, "parse format template")

	log.Print(*format)

	var stat Stat

	var needle = wd
	var root string
	for {
		parent, name := filepath.Split(needle)
		if parent == needle {
			break
		}
		parent = strings.TrimRight(parent, string([]rune{filepath.Separator}))
		if name == ".git" {
			root = parent
			break
		}

		_, err := os.Stat(filepath.Join(needle, ".git"))
		if os.IsNotExist(err) {
			needle = parent
			continue
		}
		assertError(err, "stat current directory")
		root = needle
		break
	}
	stat.Base = root

	subdir, err := filepath.Rel(root, wd)
	assertError(err, "get rel path from root")
	stat.Subdir = subdir

	rep, err := git.PlainOpen(root)
	assertError(err, "open a repository")

	staged := false
	unstaged := false
	untracked := false
	statuses := scan(runGit("status", "--porcelain"))
	for statuses.Scan() {
		line := []rune(statuses.Text())
		if len(line) < 2 {
			continue
		}
		if line[0] == '?' || line[1] == '?' {
			untracked = true
		}
		if line[0] == 'M' || line[0] == 'D' || line[0] == 'R' || line[0] == 'A' {
			staged = true
		}
		if line[1] == 'M' || line[1] == 'D' {
			unstaged = true
		}
	}
	stat.Staged = staged
	stat.Unstaged = unstaged
	stat.Untracked = untracked

	// see https://git-scm.com/docs/git-config#FILES
	confPaths := []string{
		"/etc/gitconfig",
		filepath.Join(xdg.ConfigHome(), "git", "config"),
		os.ExpandEnv("$HOME/.gitconfig"),
		filepath.Join(root, ".git", "config"),
	}
	var conf config.Config
	for _, path := range confPaths {
		func() {
			file, err := os.Open(path)
			if os.IsNotExist(err) {
				return
			}
			assertError(err, "load config")
			defer file.Close()
			dec := config.NewDecoder(file)
			assertError(dec.Decode(&conf), "decode config %q", path)
		}()
	}
	stat.Email = conf.Section("user").Option("email")

	stash, err := stashCount(root)
	assertError(err, "open stash log")
	stat.StashCount = stash

	stat.BaseName = filepath.Base(root)

	head, err := rep.Head()
	if err != nil {
		log.Print(err)
	} else {
		stat.Branch = head.Name().Short()
		stat.Revision = head.Hash().String()

		localConf, err := rep.Config()
		assertError(err, "get local config")
		remoteName := localConf.Raw.Section("branch").Subsection(head.Name().Short()).Option("remote")
		remote := localConf.Remotes[remoteName]
		var upstreamName plumbing.ReferenceName
		var repoName string
		if remote != nil {
			for _, u := range remote.URLs {
				if strings.HasPrefix(u, "https://github.com/") {
					repoName = strings.TrimSuffix(strings.TrimPrefix(u, "https://github.com/"), ".git")
				}
			}
			for _, f := range remote.Fetch {
				if f.Match(head.Name()) {
					upstreamName = f.Dst(head.Name())
					break
				}
			}
		}
		if repoName != "" {
			stat.BaseName = repoName
		}

		headCommit, err := rep.CommitObject(head.Hash())
		assertError(err, "get a last commit")
		stat.LastEmail = headCommit.Author.Email
		stat.LastMessage = strings.TrimSpace(headCommit.Message)

		upstream, err := rep.Reference(upstreamName, true)

		if err == nil {
			upstreamCommit, err := rep.CommitObject(upstream.Hash())
			assertError(err, "get a last commit on upstream")

			stat.Upstream = upstreamName.Short()
			behinds, err := countCommit(rep, upstreamCommit, headCommit)
			assertError(err, "traverse behind objects from upstream")
			stat.Behind = behinds

			aheads, err := countCommit(rep, headCommit, upstreamCommit)
			assertError(err, "traverse ahead objects from upstream")
			stat.Ahead = aheads
		} else {
			log.Printf("failed to get upstream: %s", err)
		}

		var baseBranchRef *plumbing.Reference
		baseBranchName := "origin/master"
		shortHead := head.Name().Short()
		matchLength := 0
		references, err := rep.References()
		assertError(err, "fetch references")
		defer references.Close()
		assertError(references.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name()
			if !name.IsBranch() {
				return nil
			}
			short := name.Short()
			last := short
			if name.IsRemote() {
				terms := strings.SplitN(short, "/", 2)
				if len(terms) < 2 {
					return nil
				}
				last = terms[1]
			}
			if len(last) < matchLength {
				return nil
			}
			if strings.HasPrefix(shortHead, last+"-") {
				matchLength = len(last)
				baseBranchRef = ref
				baseBranchName = short
			}
			return nil
		}), "traverse references")
		stat.BaseBranch = baseBranchName

		if baseBranchRef == nil {
			ref, err := rep.Reference(plumbing.ReferenceName("refs/remotes/origin/master"), true)
			assertError(err, "get origin/master ref")
			baseBranchRef = ref
		}

		baseBranchCommit, err := rep.CommitObject(baseBranchRef.Hash())
		assertError(err, "get a last commit on base branch")

		baseBehinds, err := countCommit(rep, baseBranchCommit, headCommit)
		assertError(err, "traverse behind objects from base branch")
		stat.BaseBehind = baseBehinds
		// # (%a) action
	}

	{
		buf, _ := json.Marshal(stat)
		log.Print(string(buf))
	}
	assertError(tmp.Execute(os.Stdout, stat), "output stats")
}

func stashCount(dir string) (int, error) {
	stashLog, err := os.Open(filepath.Join(dir, ".git", "logs", "refs", "stash"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer stashLog.Close()
	return lineCounter(stashLog)
}

func lineCounter(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

func runGit(args ...string) []byte {
	command := exec.Command("git", args...)
	output, err := command.Output()
	assertError(err, "run git")
	if output == nil {
		return []byte{}
	}
	return output
}

func scan(buf []byte) *bufio.Scanner {
	return bufio.NewScanner(bytes.NewReader(buf))
}
