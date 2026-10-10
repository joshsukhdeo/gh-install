package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/joshsukhdeo/gh-pt/cmd"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/posener/complete"
	"github.com/willabides/kongplete"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Preprocess args to add defaults for show flags without values
	preprocessShowArgs()

	// Active Threat AI Sandbox: If an ancestor process is gh-pt, lock down execution strictly to helper commands
	if isRestricted, err := cmd.CheckAncestorForGhPt(); err == nil && isRestricted {
		if !cmd.IsHelperInvocation(os.Args[1:]) {
			fmt.Fprintln(os.Stderr, "Error: Restricted AI execution context detected (gh-pt ancestor present). Only 'gh-pt helper ...' commands are permitted.")
			return 126
		}
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: GitHub CLI ('gh') is not installed or not in PATH. It is required for gh-pt. Please install it from https://cli.github.com/")
		return 1
	}

	if len(os.Args) == 1 {
		os.Args = append(os.Args, "--help")
	}
	var cli params.CLI
	cfg, _ := config.LoadConfig()

	vars := kong.Vars{
		"install_types": cmd.GetDefaultInstallTypes(),
		"install_path":  cmd.GetDefaultTargetPath(),
		"clone_path":    cmd.GetDefaultClonePath(),
		"fork_path":     cmd.GetDefaultForkPath(),
		"version":       "2.0.0",
		"extractor":     "default",
	}

	if cfg != nil {
		if cfg.Core.InstallTypes != "" {
			vars["install_types"] = cfg.Core.InstallTypes
		}
		if cfg.Paths.InstallPath != "" {
			vars["install_path"] = cfg.Paths.InstallPath
		}
		if cfg.Paths.ClonePath != "" {
			vars["clone_path"] = cfg.Paths.ClonePath
		}
		if cfg.Core.Extractor != "" {
			vars["extractor"] = cfg.Core.Extractor
		}
		if cfg.Paths.ForkPath != "" {
			vars["fork_path"] = cfg.Paths.ForkPath
		}
	}

	parser := kong.Must(&cli,
		kong.Name(filepath.Base(os.Args[0])),
		kong.Description(`Install binaries for a Github repository release interactively or non-interactively.`),
		kong.DefaultEnvars(cmd.GetEnvPrefix()),
		vars)

	kongplete.Complete(parser,
		kongplete.WithPredictor("file", complete.PredictFiles("*")),
		kongplete.WithPredictor("installed_apps", cmd.PredictInstalledApps),
		kongplete.WithPredictor("github_repos", cmd.PredictGithubRepos),
		kongplete.WithPredictor("config_keys", cmd.PredictConfigKeys),
		kongplete.WithPredictor("extractors", cmd.PredictExtractors),
		kongplete.WithPredictor("progress_bars", cmd.PredictProgressBars),
		kongplete.WithPredictor("sidecar_modes", cmd.PredictSidecarModes),
	)
	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	if cli.Test {
		importJsonBytes, _ := json.MarshalIndent(cli, "", "  ")
		fmt.Println(string(importJsonBytes))
		return 0
	}

	err = cmd.RunCommand(ctx.Command(), &cli)
	if err != nil {
		noColor := os.Getenv("NO_COLOR") != ""
		if strings.HasPrefix(err.Error(), "Warning:") {
			if noColor {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\033[33m%v\033[0m\n", err)
			}
		} else {
			if noColor {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
			}
		}
		return 1
	}
	return 0
}

// preprocessShowArgs adds default values to show flags when used without values.
// --versions → --versions 24
// --assets → --assets 50
// --description → --description 10
// --readme → --readme 50
func preprocessShowArgs() {
	defaults := map[string]string{
		"--versions":    "24",
		"--assets":      "50",
		"--description": "10",
		"--readme":      "50",
	}

	var result []string
	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]

		// Check if this is one of our show flags
		if def, ok := defaults[arg]; ok {
			result = append(result, arg)
			// Check if next arg exists and is not a flag
			if i+1 >= len(os.Args) || strings.HasPrefix(os.Args[i+1], "-") {
				// No value provided, add default
				result = append(result, def)
			}
		} else {
			result = append(result, arg)
		}
	}
	os.Args = result
}
