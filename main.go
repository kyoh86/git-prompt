package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
	"strings"

	flags "github.com/jessevdk/go-flags"
	"github.com/kyoh86/xdg"
	"github.com/wacul/ulog"
	"github.com/wacul/ulog/adapter/stdlog"

	rungit "github.com/kyoh86/git-prompt/git"
	"gopkg.in/src-d/go-git.v4/plumbing/format/config"
)

func assertError(ctx context.Context, err error, doing string, args ...interface{}) {
	if err != nil {
		logger := ulog.Logger(ctx)
		logger.WithField("error", err).Error("failed to " + fmt.Sprintf(doing, args...))
		panic(err)
	}
}

func assertSetBool(ctx context.Context, getter func() (bool, error), result *bool, doing string, args ...interface{}) {
	flag, err := getter()
	assertError(ctx, err, doing, args...)
	*result = flag
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

func main() {
	styles := map[string]string{
		"zsh": `%F{yellow}
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
				%F{yellow}({{.BaseBranch}}%f%F{red}-{{.BaseBehind}}%f%F{yellow})%f
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
			{{- if eq .Upstream "" -}}
				%F{red}⚑%f
			{{- end -}}
			%F{blue}]%f`,

		"tmux": `#[bg=black]#[fg=yellow]
			{{- if eq .Staged true -}}    + {{- end -}}
			{{- if eq .Unstaged true -}}  - {{- end -}}
			{{- if eq .Untracked true -}} ? {{- end -}}
			{{- if and (eq .LastMessage "wip") (eq .Email .LastEmail) -}}
			#[fg=red]!wip!
			{{- end -}}
			{{- if gt .Ahead 0 -}}  #[fg=red]⬆ {{.Ahead}}      {{- end -}}
			{{- if gt .Behind 0 -}} #[fg=magenta]⬇ {{.Behind}} {{- end -}}
			{{- if gt .BaseBehind 0 -}}
			#[fg=yellow]({{.BaseBranch}}#[fg=red]-{{.BaseBehind}}#[fg=yellow])
			{{- end -}}
			{{- if gt .StashCount 0 -}}
			#[fg=yellow]♻ {{.StashCount}}
			{{- end}} #[fg=blue][{{.BaseName}}
			{{- if ne .Subdir "." -}}
			#[fg=yellow]/{{.Subdir}}
			{{- end -}}
			{{- if and (ne .Branch "master") (ne .Branch "") -}}
			#[fg=green]:{{.Branch}}
			{{- end -}}
			{{- if eq .Upstream "" -}}#[fg=red]⚑{{end -}}
			#[fg=blue]]#[fg=default]`,
	}

	var option struct {
		Dir     string `long:"dir" short:"C" description:"working directory"`
		Style   string `long:"style" short:"s" description:"output style" default:"pretty"`
		Verbose []bool `long:"verbose" short:"v" description:"log verbose"`
	}

	ctx := context.Background()
	logger := ulog.Logger(ctx)

	if _, err := flags.ParseArgs(&option, os.Args[1:]); err != nil {
		panic(err)
	}

	var level ulog.Level
	switch len(option.Verbose) {
	case 0:
		l, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_USER, "git-prompt")
		if err != nil {
			panic(err)
		}

		level = ulog.WarnLevel
		log.SetOutput(l)
	case 1:
		level = ulog.InfoLevel
		log.SetOutput(os.Stderr)
	default:
		level = ulog.DebugLevel
		log.SetOutput(os.Stderr)
	}

	logger = logger.WithAdapter(&stdlog.Adapter{Level: level})
	ctx = logger

	if option.Dir == "" {
		wd, err := os.Getwd()
		assertError(ctx, err, "get working directory")
		option.Dir = wd
	}

	var format string
	var pretty bool
	switch {
	case strings.HasPrefix(option.Style, "format:"):
		format = strings.TrimPrefix(option.Style, "format:")
	case strings.HasPrefix(option.Style, "f:"):
		format = strings.TrimPrefix(option.Style, "f:")
	case option.Style == "pretty":
		format = ""
		pretty = true
	default:
		format = styles[option.Style]
	}

	tmp, tmpErr := template.New("stat").Parse(format)
	assertError(ctx, tmpErr, "parse format template")

	var stat Stat

	repo, repoErr := rungit.OpenDir(option.Dir)
	assertError(ctx, repoErr, "open a repository")
	stat.Base = repo.Root()

	{
		subdir, err := filepath.Rel(stat.Base, option.Dir)
		assertError(ctx, err, "get rel path from root")
		stat.Subdir = subdir
	}

	assertSetBool(ctx, repo.Staged, &stat.Staged, "get staged")
	assertSetBool(ctx, repo.Unstaged, &stat.Unstaged, "get unstaged")
	assertSetBool(ctx, repo.Untracked, &stat.Untracked, "get untracked")

	// see https://git-scm.com/docs/git-config#FILES
	confPaths := []string{
		"/etc/gitconfig",
		filepath.Join(xdg.ConfigHome(), "git", "config"),
		os.ExpandEnv("$HOME/.gitconfig"),
		filepath.Join(stat.Base, ".git", "config"),
	}
	var conf config.Config
	for _, path := range confPaths {
		func() {
			file, err := os.Open(path)
			if os.IsNotExist(err) {
				return
			}
			assertError(ctx, err, "load config")
			defer file.Close()
			dec := config.NewDecoder(file)
			assertError(ctx, dec.Decode(&conf), "decode config %q", path)
		}()
	}
	stat.Email = conf.Section("user").Option("email")

	{
		stash, err := repo.StashCount()
		assertError(ctx, err, "open stash log")
		stat.StashCount = stash
	}

	stat.BaseName = filepath.Base(stat.Base)

	{
		lastEmail, lastMessage, lastHash, err := repo.LastCommit()
		assertError(ctx, err, "get last commit")
		stat.LastEmail = lastEmail
		stat.LastMessage = strings.TrimSpace(lastMessage)
		stat.Revision = lastHash
	}

	{
		branch, err := repo.CurrentBranch()
		assertError(ctx, err, "get current branch")
		stat.Branch = branch
		if stat.Branch == "HEAD" {
			stat.Branch = string(([]rune(stat.Revision))[:6]) + "..."
		}
	}
	{
		remote, err := repo.Remote(stat.Branch)
		assertError(ctx, err, "search remote")

		remoteURL, err := repo.RemoteURL(remote)
		assertError(ctx, err, "search remoteURL")
		if strings.HasPrefix(remoteURL, "https://github.com/") {
			stat.BaseName = strings.TrimSuffix(strings.TrimPrefix(remoteURL, "https://github.com/"), ".git")
		}
	}

	{
		upstream, err := repo.Upstream()
		assertError(ctx, err, "search upstream")
		stat.Upstream = upstream
	}

	{
		ahead, err := repo.AheadCount(stat.Branch)
		assertError(ctx, err, "count ahead")
		stat.Ahead = ahead
	}

	{
		behind, err := repo.BehindCount(stat.Branch)
		assertError(ctx, err, "count behind")
		stat.Behind = behind
	}

	{
		baseBranch, err := repo.BaseBranch(stat.Branch)
		assertError(ctx, err, "search base branch")
		stat.BaseBranch = baseBranch
	}

	{
		baseBehinds, err := repo.BehindCountFrom(stat.Branch, stat.BaseBranch)
		assertError(ctx, err, "traverse behind objects from base branch")
		stat.BaseBehind = baseBehinds
	}
	// TODO: # (%a) action

	if pretty {
		writer := json.NewEncoder(os.Stdout)
		writer.SetIndent("", "  ")
		assertError(ctx, writer.Encode(stat), "output pretty")
	}

	assertError(ctx, tmp.Execute(os.Stdout, stat), "output stats")
}
