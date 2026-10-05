package cmd

import (
	"fmt"
	"strings"

	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/state"
)

func RunCommand(cmdStr string, cli *params.CLI) error {
	r := &RootCLI{}

	r.LogLevel = cli.LogLevel
	r.LogFormat = cli.LogFormat
	r.LogQuietInteractive = cli.LogQuietInteractive
	r.Verbose = cli.Verbose

	switch cmdStr {
	case "install", "install <repository>":
		r.CommonInstallFlags = cli.Install.CommonInstallFlags
		r.Repository = cli.Install.Repository
		return r.RunInstall()
	case "ls", "ls <filter>":
		return ListState(&RootCLI{ExecContext: params.ExecContext{Ls: cli.Ls.Filter, ListFormat: cli.Ls.Format, CommonInstallFlags: params.CommonInstallFlags{Global: cli.Ls.Global}}})
	case "ll", "ll <filter>":
		return ListState(&RootCLI{ExecContext: params.ExecContext{Ll: cli.Ll.Filter, Full: true, ListFormat: cli.Ll.Format, CommonInstallFlags: params.CommonInstallFlags{Global: cli.Ll.Global}}})
	case "rm", "rm <target>":

		if cli.Rm.StateOnly {
			return RmStateOnly(cli.Rm.Target)
		}
		return RemoveApp(cli.Rm.Target, cli.Rm.Purge)
	case "upgrade", "upgrade <repository>":
		r.Repository = cli.Upgrade.Repository
		r.DisablePrompts = cli.Upgrade.DisablePrompts
		r.Extractor = cli.Upgrade.Extractor
		r.Overwrite = cli.Upgrade.Overwrite
		r.DryRun = cli.Upgrade.DryRun
		r.VerifyChecksum = cli.Upgrade.VerifyChecksum
		r.SkipVtSandbox = cli.Upgrade.SkipVtSandbox
		r.FallbackReleases = cli.Upgrade.FallbackReleases
		r.WarnUnmappedAssets = cli.Upgrade.WarnUnmappedAssets
		r.ProgressBar = cli.Upgrade.ProgressBar
		r.IsUpgradeCmd = true
		if !cli.Upgrade.User && !cli.Upgrade.Global {
			r.UpdateAll = true
		} else {
			r.Update = true
			r.Global = cli.Upgrade.Global
		}
		return r.RunInstall()
	case "search", "search <query>":
		return cli.Search.Run(&r.ExecContext)
	case "state cat":
		return StateCat()
	case "state view":
		return StateView()
	case "state add", "state add <repository>":
		app := &state.InstalledApp{
			Version:      cli.State.Add.ReleaseVersion,
			TargetPath:   cli.State.Add.TargetPath,
			Global:       cli.State.Add.Global,
			Pinned:       cli.State.Add.Pinned,
			Extractor:    cli.State.Add.Extractor,
			ReleaseAsset: cli.State.Add.ReleaseAsset,
		}
		if cli.State.Add.Type != "" {
			app.Type = strings.Split(cli.State.Add.Type, ",")
		}
		return StateAdd(cli.State.Add.Repository, app, cli.State.Add.Force)
	case "state rm", "state rm <target>":
		return StateRm(cli.State.Rm.Target, cli.State.Rm.Force)
	case "state update", "state update <target>", "state update <target> <fields>":
		updates, err := ParseStateUpdate(cli.State.Update.Fields)
		if err != nil {
			return err
		}
		return StateUpdate(cli.State.Update.Target, updates)
	case "state edit":
		return StateEdit()
	case "show", "show <repository>":
		return ShowInfo(&RootCLI{ExecContext: params.ExecContext{Repository: cli.Show.Repository, Show: true, ShowAssets: cli.Show.Assets, ShowVersions: cli.Show.Versions, ShowDescription: cli.Show.Description, ShowReadme: cli.Show.Readme, DiscoverSidecars: cli.Show.DiscoverSidecars, CommonInstallFlags: params.CommonInstallFlags{ReleaseVersion: cli.Show.Version, Stable: cli.Show.Stable, Prerelease: cli.Show.Prerelease}}})
	case "repo", "repo clone", "repo clone <repository>", "repo fork", "repo fork <repository>":
		// Handle both repo clone and repo fork subcommands
		// kong populates either cli.Repo.Clone or cli.Repo.Fork based on subcommand used
		if cli.Repo.Clone.Repository != "" {
			r.CommonInstallFlags = cli.Install.CommonInstallFlags
			r.Repository = cli.Repo.Clone.Repository
			r.Clone = true
			r.Overwrite = cli.Repo.Clone.Force
			r.MaxDepth = cli.Repo.Clone.MaxDepth
			if cli.Repo.Clone.TargetPath != "" {
				r.TargetPath = cli.Repo.Clone.TargetPath
			}
			return r.RunInstall()
		}
		if cli.Repo.Fork.Repository != "" {
			r.CommonInstallFlags = cli.Install.CommonInstallFlags
			r.Repository = cli.Repo.Fork.Repository
			r.Fork = true
			r.Overwrite = cli.Repo.Fork.Force
			r.MaxDepth = cli.Repo.Fork.MaxDepth
			if cli.Repo.Fork.TargetPath != "" {
				r.TargetPath = cli.Repo.Fork.TargetPath
			}
			return r.RunInstall()
		}
		// If no repository specified, check if it was passed via env var
		// kong should have populated it from GH_PT_REPOSITORY env var
		if cli.Repo.Clone.Repository == "" && cli.Repo.Fork.Repository == "" {
			// Try to determine which subcommand was intended
			// Default to clone if neither has repository
			return fmt.Errorf("repository argument is required for repo clone/fork")
		}
		return fmt.Errorf("unknown repo subcommand")
	case "source", "source <repository>":
		r.CommonInstallFlags = cli.Source.CommonInstallFlags
		r.Repository = cli.Source.Repository
		r.CompileFromSource = true
		r.AI = true
		aiCmd := cli.Source.AICmd
		if aiCmd == "" {
			cfg, _ := config.LoadConfig()
			if cfg != nil {
				if cli.Source.Interactive {
					aiCmd = cfg.AI.AIInteractiveCmd
				} else {
					aiCmd = cfg.AI.AICmd
				}
			}
		}
		r.AICmd = aiCmd
		return r.RunInstall()
	case "scan ai":
		aiCmd := cli.Scan.Ai.AICmd
		if aiCmd == "" {
			cfg, _ := config.LoadConfig()
			if cfg != nil {
				if cli.Scan.Ai.Interactive {
					aiCmd = cfg.AI.AIInteractiveCmd
				} else {
					aiCmd = cfg.AI.AICmd
				}
			}
		}
		return RunAIScan(cli.Scan.Ai.Target, aiCmd)
	case "scan vt":
		return RunVTScan(cli.Scan.Vt.Target)
	case "vt set-key":
		return SetVTKey(cli.Vt.SetKey.Key)
	case "config ls":
		return ConfigLs()
	case "config get":
		return ConfigGet(cli.Config.Get.Key)
	case "config set":
		return ConfigSet(cli.Config.Set.Key, cli.Config.Set.Value)
	case "config rm":
		return ConfigRm(cli.Config.Rm.Key)
	case "config menu":
		return ConfigMenu()
	case "completions bash":
		return GenerateCompletions("bash")
	case "completions zsh":
		return GenerateCompletions("zsh")
	case "completions powershell":
		return GenerateCompletions("powershell")
	case "helper":
		return RunHelper(&cli.Helper)
	default:
		return fmt.Errorf("unknown command: %s", cmdStr)
	}
}
