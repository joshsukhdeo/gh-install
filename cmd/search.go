package cmd

import (
	"os"
	"os/exec"

	"github.com/joshsukhdeo/gh-pt/params"
)

type SearchCmd struct {
	Query       string `arg:"" predictor:"github_repos" predict:"github_repos"`
	Description bool   `short:"d"`
}

func init() {
	params.SearchRunner = func(c *params.SearchCmd, ctx *params.ExecContext) error {
		cmd := &SearchCmd{
			Query:       c.Query,
			Description: c.Description,
		}
		return cmd.Run(ctx)
	}
}

func (c *SearchCmd) Run(ctx *params.ExecContext) error {
	args := []string{"search", "repos", c.Query}
	if c.Description {
		args = append(args, "--match", "name,description")
	} else {
		args = append(args, "--match", "name")
	}

	cmd := exec.Command("gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
