package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	flags "github.com/jessevdk/go-flags"
	"github.com/kyoh86/git-prompt/git"
	"github.com/kyoh86/git-prompt/log"
	"github.com/wacul/ulog"
)

func assertError(ctx context.Context, err error, doing string, args ...interface{}) {
	if err != nil {
		logger := ulog.Logger(ctx)
		logger.WithField("error", err).Error("failed to " + fmt.Sprintf(doing, args...))
		panic(err)
	}
}

// Stat holds git statuses
type Stat struct {
	Root        string
	Name        string
	Subdir      string
	Branch      string
	Hash        string
	Staged      bool
	Unstaged    bool
	Untracked   bool
	Email       string
	StashCount  int
	LastEmail   string
	LastMessage string
	Wip         bool
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
			{{- if and .Wip (eq .Email .LastEmail) -}}
				%F{red}!wip!%f
			{{- end -}}
			{{- if gt .Ahead 0 -}}  %F{red}⬆ {{.Ahead}}%f      {{- end -}}
			{{- if gt .Behind 0 -}} %F{magenta}⬇ {{.Behind}}%f {{- end -}}
			{{- if gt .BaseBehind 0 -}}
				%F{yellow}({{.BaseBranch}}%f%F{red}-{{.BaseBehind}}%f%F{yellow})%f
			{{- end -}}
			{{- if gt .StashCount 0 -}}
				%F{yellow}♻ {{.StashCount}}%f
			{{- end}} %F{blue}[{{.Name}}%f
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
			{{- if and .Wip (eq .Email .LastEmail) -}}
			#[fg=red]!wip!
			{{- end -}}
			{{- if gt .Ahead 0 -}}  #[fg=red]⬆ {{.Ahead}}      {{- end -}}
			{{- if gt .Behind 0 -}} #[fg=magenta]⬇ {{.Behind}} {{- end -}}
			{{- if gt .BaseBehind 0 -}}
			#[fg=yellow]({{.BaseBranch}}#[fg=red]-{{.BaseBehind}}#[fg=yellow])
			{{- end -}}
			{{- if gt .StashCount 0 -}}
			#[fg=yellow]♻ {{.StashCount}}
			{{- end}} #[fg=blue][{{.Name}}
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

	if _, err := flags.ParseArgs(&option, os.Args[1:]); err != nil {
		panic(err)
	}

	ctx := log.Background(option.Verbose)

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

	repo, repoErr := git.OpenDir(option.Dir)
	if repoErr == git.ErrIsNotInWorkingDirectory {
		return
	}
	assertError(ctx, repoErr, "open a repository")
	defer repo.Close()
	stat.Root = repo.Root()
	stat.Name = filepath.Base(stat.Root)

	{
		subdir, err := filepath.Rel(stat.Root, option.Dir)
		assertError(ctx, err, "get rel path from root")
		stat.Subdir = subdir
	}

	assertError(ctx, repo.StagedVar(&stat.Staged), "get staged")
	assertError(ctx, repo.UnstagedVar(&stat.Unstaged), "get unstaged")
	assertError(ctx, repo.UntrackedVar(&stat.Untracked), "get untracked")
	assertError(ctx, repo.EmailVar(&stat.Email), "get user account")
	assertError(ctx, repo.StashCountVar(&stat.StashCount), "open stash log")
	assertError(ctx, repo.LastCommitHashVar(&stat.Hash), "get last commit hash")
	assertError(ctx, repo.UpstreamVar(&stat.Upstream), "search upstream")
	assertError(ctx, repo.AheadCountVar(&stat.Ahead), "count ahead")
	assertError(ctx, repo.BehindCountVar(&stat.Behind), "count behind")
	assertError(ctx, repo.BranchVar(&stat.Branch), "get current branch")
	assertError(ctx, repo.LastCommitterVar(&stat.LastEmail), "get last committer")
	assertError(ctx, repo.LastCommitMessageVar(&stat.LastMessage), "get last commit message")
	wipRegexp := regexp.MustCompile(`^wip(\W|$)`)
	if wipRegexp.MatchString(stat.LastMessage) {
		stat.Wip = true
	}

	if stat.Branch == "HEAD" {
		stat.Branch = string(([]rune(stat.Hash))[:6]) + "..."
	}
	{
		remote, err := repo.Remote(stat.Branch)
		assertError(ctx, err, "search remote")

		remoteURL, err := repo.RemoteURL(remote)
		assertError(ctx, err, "search remoteURL")
		if strings.HasPrefix(remoteURL, "https://github.com/") {
			stat.Name = strings.TrimSuffix(strings.TrimPrefix(remoteURL, "https://github.com/"), ".git")
		}
	}
	{
		baseBranch, err := repo.BaseBranch(stat.Branch)
		assertError(ctx, err, "search base branch")
		stat.BaseBranch = baseBranch
	}

	{
		baseBehinds, err := repo.BehindCountFrom(stat.BaseBranch)
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
