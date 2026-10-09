package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/release"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/pterm/pterm"
)

func ConfigLs() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	maskKey := func(key string) string {
		if key == "" {
			return ""
		}
		if len(key) <= 8 {
			return "********"
		}
		return key[:4] + "..." + key[len(key)-4:]
	}

	tableData := pterm.TableData{
		{"Category", "Setting", "Value"},
		// Paths
		{"Paths", "install_path", cfg.Paths.InstallPath},
		{"Paths", "global_path", cfg.Paths.GlobalPath},
		{"Paths", "clone_path", cfg.Paths.ClonePath},
		{"Paths", "fork_path", cfg.Paths.ForkPath},
		{"Paths", "sidecar_path", cfg.Paths.SidecarPath},
		{"Paths", "target_base_dir", cfg.Paths.TargetBaseDir},
		{"Paths", "repo_dir", cfg.Paths.RepoDir},
		{"Paths", "package_path", cfg.Paths.PackagePath},
		// AI
		{"AI", "ai_cmd", cfg.AI.AICmd},
		{"AI", "ai_interactive_cmd", cfg.AI.AIInteractiveCmd},
		// Core
		{"Core", "install_types", cfg.Core.InstallTypes},
		{"Core", "extractor", cfg.Core.Extractor},
		{"Core", "progress_bar", cfg.Core.ProgressBar},
		{"Core", "wine", cfg.Core.Wine},
		{"Core", "avx_level", cfg.Core.AvxLevel},
		{"Core", "allow_prerelease", fmt.Sprintf("%t", cfg.Core.AllowPrerelease)},
		{"Core", "add_deps", fmt.Sprintf("%t", cfg.Core.AddDeps)},
		{"Core", "resolve_deps", fmt.Sprintf("%t", cfg.Core.ResolveDeps)},
		{"Core", "no_deps", fmt.Sprintf("%t", cfg.Core.NoDeps)},
		{"Core", "disable_prompts", fmt.Sprintf("%t", cfg.Core.DisablePrompts)},
		{"Core", "no_save_state", fmt.Sprintf("%t", cfg.Core.NoSaveState)},
		{"Core", "global", fmt.Sprintf("%t", cfg.Core.Global)},
		{"Core", "symlink", fmt.Sprintf("%t", cfg.Core.Symlink)},
		{"Core", "keep_suffixes", fmt.Sprintf("%t", cfg.Core.KeepSuffixes)},
		{"Core", "disable_icons", fmt.Sprintf("%t", cfg.Core.DisableIcons)},
		{"Core", "no_color", fmt.Sprintf("%t", cfg.Core.NoColor)},
		{"Core", "no_emojis", fmt.Sprintf("%t", cfg.Core.NoEmojis)},
		{"Core", "log_to_file", fmt.Sprintf("%t", cfg.Core.LogToFile)},
		{"Core", "vt_api_key", maskKey(cfg.Core.VTApiKey)},
	}

	pterm.DefaultHeader.WithFullWidth().Println("gh-pt configuration (" + config.GetConfigPath() + ")")
	return pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(tableData).Render()
}

func ConfigGet(key string) error {
	return fmt.Errorf("not fully implemented")
}

func ConfigSet(key, val string) error {
	return fmt.Errorf("not fully implemented")
}

func ConfigRm(key string) error {
	return fmt.Errorf("not fully implemented")
}

func ConfigMenu() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for {
		category, _ := pterm.DefaultInteractiveSelect.
			WithOptions([]string{"Paths", "AI", "Core", "Exit"}).
			WithDefaultText("Select Configuration Category").
			Show()

		if category == "Exit" {
			break
		}

		handleCategoryMenu(cfg, category)

		if err := config.SaveConfig(cfg); err != nil {
			pterm.Error.Printf("Failed to save config: %v\n", err)
		} else {
			pterm.Success.Println("Configuration saved successfully.")
		}
	}

	return nil
}

func handleCategoryMenu(cfg *config.Config, category string) {
	for {
		var options []string
		var selected string

		switch category {
		case "Paths":
			options = []string{
				fmt.Sprintf("InstallPath: %s", cfg.Paths.InstallPath),
				fmt.Sprintf("GlobalPath: %s", cfg.Paths.GlobalPath),
				fmt.Sprintf("ClonePath: %s", cfg.Paths.ClonePath),
				fmt.Sprintf("ForkPath: %s", cfg.Paths.ForkPath),
				"Back",
			}
		case "AI":
			options = []string{
				fmt.Sprintf("AICmd: %s", cfg.AI.AICmd),
				fmt.Sprintf("AIInteractiveCmd: %s", cfg.AI.AIInteractiveCmd),
				"Back",
			}
		case "Core":
			options = []string{
				fmt.Sprintf("InstallTypes: %s", cfg.Core.InstallTypes),
				fmt.Sprintf("ResolveDeps: %t", cfg.Core.ResolveDeps),
				fmt.Sprintf("NoDeps: %t", cfg.Core.NoDeps),
				fmt.Sprintf("DisablePrompts: %t", cfg.Core.DisablePrompts),
				fmt.Sprintf("NoSaveState: %t", cfg.Core.NoSaveState),
				fmt.Sprintf("Wine: %s", cfg.Core.Wine),
				fmt.Sprintf("Extractor: %s", cfg.Core.Extractor),
				fmt.Sprintf("KeepSuffixes: %t", cfg.Core.KeepSuffixes),
				fmt.Sprintf("VTApiKey: %s", cfg.Core.VTApiKey),
				fmt.Sprintf("AllowPrerelease: %t", cfg.Core.AllowPrerelease),
				fmt.Sprintf("DisableIcons: %t", cfg.Core.DisableIcons),
				fmt.Sprintf("LogToFile: %t", cfg.Core.LogToFile),
				fmt.Sprintf("Symlink: %t", cfg.Core.Symlink),
				"Back",
			}
		}

		selected, _ = pterm.DefaultInteractiveSelect.
			WithOptions(options).
			WithDefaultText(fmt.Sprintf("Select %s Field to Edit", category)).
			Show()

		if selected == "Back" {
			break
		}

		editField(cfg, category, selected)
	}
}

func editField(cfg *config.Config, category, selected string) {
	// Parse the field name from the selected option
	parts := strings.Split(selected, ":")
	if len(parts) == 0 {
		return
	}
	fieldName := strings.TrimSpace(parts[0])

	switch category {
	case "Paths":
		switch fieldName {
		case "InstallPath":
			cfg.Paths.InstallPath, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("InstallPath").WithDefaultValue(cfg.Paths.InstallPath).Show()
		case "GlobalPath":
			cfg.Paths.GlobalPath, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("GlobalPath").WithDefaultValue(cfg.Paths.GlobalPath).Show()
		case "ClonePath":
			cfg.Paths.ClonePath, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("ClonePath").WithDefaultValue(cfg.Paths.ClonePath).Show()
		case "ForkPath":
			cfg.Paths.ForkPath, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("ForkPath").WithDefaultValue(cfg.Paths.ForkPath).Show()
		}
	case "AI":
		switch fieldName {
		case "AICmd":
			cfg.AI.AICmd, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("AICmd").WithDefaultValue(cfg.AI.AICmd).Show()
		case "AIInteractiveCmd":
			cfg.AI.AIInteractiveCmd, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("AIInteractiveCmd").WithDefaultValue(cfg.AI.AIInteractiveCmd).Show()
		}
	case "Core":
		switch fieldName {
		case "InstallTypes":
			cfg.Core.InstallTypes, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("InstallTypes").WithDefaultValue(cfg.Core.InstallTypes).Show()
		case "Wine":
			cfg.Core.Wine, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("Wine").WithDefaultValue(cfg.Core.Wine).Show()
		case "VTApiKey":
			cfg.Core.VTApiKey, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("VTApiKey").WithDefaultValue(cfg.Core.VTApiKey).Show()
		case "ResolveDeps":
			cfg.Core.ResolveDeps, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("ResolveDeps").WithDefaultValue(cfg.Core.ResolveDeps).Show()
		case "NoDeps":
			cfg.Core.NoDeps, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("NoDeps").WithDefaultValue(cfg.Core.NoDeps).Show()
		case "DisablePrompts":
			cfg.Core.DisablePrompts, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("DisablePrompts").WithDefaultValue(cfg.Core.DisablePrompts).Show()
		case "NoSaveState":
			cfg.Core.NoSaveState, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("NoSaveState").WithDefaultValue(cfg.Core.NoSaveState).Show()
		case "Extractor":
			cfg.Core.Extractor, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("Extractor").WithDefaultValue(cfg.Core.Extractor).Show()
		case "KeepSuffixes":
			cfg.Core.KeepSuffixes, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("KeepSuffixes").WithDefaultValue(cfg.Core.KeepSuffixes).Show()
		case "AllowPrerelease":
			cfg.Core.AllowPrerelease, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("AllowPrerelease").WithDefaultValue(cfg.Core.AllowPrerelease).Show()
		case "DisableIcons":
			cfg.Core.DisableIcons, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("DisableIcons").WithDefaultValue(cfg.Core.DisableIcons).Show()
		case "LogToFile":
			cfg.Core.LogToFile, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("LogToFile").WithDefaultValue(cfg.Core.LogToFile).Show()
		case "Symlink":
			cfg.Core.Symlink, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("Symlink").WithDefaultValue(cfg.Core.Symlink).Show()
		}
	}
}

func RunAIScan(target, aiCmd string) error {
	pterm.Info.Println("Running AI scan on", target)

	prompt := fmt.Sprintf("Analyze the GitHub repository %s for safety concerns, malicious code, suspicious recent commits, or backdoors. Report your findings concisely and explicitly state if it appears safe or compromised.", target)

	if err := runAIAgent(aiCmd, prompt, ""); err != nil {
		return fmt.Errorf("AI safety scan failed to execute: %w", err)
	}

	st, err := state.LoadState()
	if err == nil {
		if app, exists := st.Apps[target]; exists {
			app.LastAIScan = time.Now().Format(time.RFC3339)
			if err := st.Save(); err != nil {
				pterm.Warning.Println("Failed to save state after AI scan")
			}
		}
	}

	pterm.Success.Printf("Successfully completed AI scan for %s\n", target)
	return nil
}

func RunVTScan(target string) error {
	pterm.Info.Println("Running VT scan on", target)

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("could not load config: %w", err)
	}

	apiKey := cfg.Core.VTApiKey
	if os.Getenv("VT_API_KEY") != "" {
		apiKey = os.Getenv("VT_API_KEY")
	}

	if apiKey == "" {
		return fmt.Errorf("virustotal api key is not set")
	}

	st, err := state.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	app, exists := st.Apps[target]
	if !exists {
		// Let's see if target is a path
		if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
			hash, err := release.CalculateSHA256(target)
			if err != nil {
				return fmt.Errorf("failed to hash %s: %w", target, err)
			}
			return release.VerifyHashWithVirusTotal(hash, target, apiKey, true, false)
		}
		return fmt.Errorf("app %s not found in state and is not a valid file path", target)
	}

	if len(app.Rename) == 0 {
		return fmt.Errorf("no binaries mapped in state for %s", target)
	}

	for _, destName := range app.Rename {
		binPath := filepath.Join(app.TargetPath, destName)
		hash, err := release.CalculateSHA256(binPath)
		if err != nil {
			pterm.Warning.Printf("Could not hash binary %s: %v\n", binPath, err)
			continue
		}

		if err := release.VerifyHashWithVirusTotal(hash, binPath, apiKey, true, false); err != nil {
			return fmt.Errorf("virustotal scan failed for %s: %w", binPath, err)
		}
	}

	app.LastVTScan = time.Now().Format(time.RFC3339)
	if err := st.Save(); err != nil {
		return fmt.Errorf("could not update state: %w", err)
	}

	pterm.Success.Printf("Successfully scanned %s and updated state.\n", target)
	return nil
}

func SetVTKey(key string) error {
	pterm.Info.Println("Setting VT key to", key)
	return nil
}
