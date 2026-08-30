package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/joshsukhdeo/gh-install/cmd"
	"github.com/joshsukhdeo/gh-install/config"
)

func main() {

	var cli cmd.RootCLI

	cfg, _ := config.LoadConfig()

	vars := kong.Vars{
		"install_types": cmd.GetDefaultInstallTypes(),
		"install_path":  cmd.GetDefaultTargetPath(),
		"version":       "2.0.0",
	}

	if cfg != nil {
		if cfg.InstallTypes != "" {
			vars["install_types"] = cfg.InstallTypes
		}
		if cfg.InstallPath != "" {
			vars["install_path"] = cfg.InstallPath
		}
	}

	ctx := kong.Parse(&cli,
		kong.Name("gh-install"),
		kong.Description(`Install binaries for a Github repository release interactively or non-interactively.  
			Intended for quickly installing release binaries for projects that do not distribute 
			using Homebrew or other package managers.`),
		kong.DefaultEnvars(cmd.GetEnvPrefix()),
		kong.PostBuild(cmd.PostBuild),
		vars)

	err := ctx.Run()
	if err != nil {
		os.Exit(1)
	}
}
