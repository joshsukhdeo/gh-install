package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshsukhdeo/gh-install/state"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog/log"
)

func ListState() error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	if len(st.Apps) == 0 {
		pterm.Info.Println("No applications are currently managed by gh-install.")
		return nil
	}

	tableData := pterm.TableData{
		{"Repository", "Version", "Scope", "Auto-Update", "Target Path"},
	}

	var repos []string
	for k := range st.Apps {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		app := st.Apps[repo]
		scope := "User"
		if app.Global {
			scope = "Global"
		}
		autoUpdate := "Enabled"
		if app.Disabled {
			autoUpdate = "Disabled"
		}

		tableData = append(tableData, []string{
			app.Repository,
			app.Version,
			scope,
			autoUpdate,
			app.TargetPath,
		})
	}

	fmt.Println()
	pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
	fmt.Println()
	return nil
}

func RmState(target string) error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	targetLower := strings.ToLower(target)
	var toRemove []string

	for repo, app := range st.Apps {
		if strings.ToLower(repo) == targetLower {
			toRemove = append(toRemove, repo)
			continue
		}
		// check if the target matches any renamed binary
		for _, renamed := range app.Rename {
			if strings.ToLower(renamed) == targetLower {
				toRemove = append(toRemove, repo)
				break
			}
		}
		// check if target matches the suffix of repo
		parts := strings.Split(repo, "/")
		if strings.ToLower(parts[len(parts)-1]) == targetLower {
			toRemove = append(toRemove, repo)
		}
	}

	if len(toRemove) == 0 {
		log.Warn().Msgf("No application found matching '%s'", target)
		return nil
	}

	for _, r := range toRemove {
		delete(st.Apps, r)
		log.Info().Msgf("Removed %s from managed state.", r)
	}

	return st.Save()
}

func EditState() error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	if len(st.Apps) == 0 {
		pterm.Info.Println("No applications are currently managed by gh-install.")
		return nil
	}

	// 1. Interactive Multi-select for toggling auto-updates
	var options []string
	var selectedOptions []string

	var repos []string
	for k := range st.Apps {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		app := st.Apps[repo]
		cat := "User"
		if app.Global {
			cat = "Global"
		}
		label := fmt.Sprintf("[%s] %s", cat, repo)
		options = append(options, label)
		if !app.Disabled {
			selectedOptions = append(selectedOptions, label)
		}
	}

	pterm.Info.Println("SPACE toggles Auto-Update. ENTER confirms selection.")
	selected, _ := pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultOptions(selectedOptions).
		WithFilter(false).
		Show("Select apps to ENABLE for automatic updates")

	// Apply selection
	selectedMap := make(map[string]bool)
	for _, s := range selected {
		selectedMap[s] = true
	}

	for _, repo := range repos {
		app := st.Apps[repo]
		cat := "User"
		if app.Global {
			cat = "Global"
		}
		label := fmt.Sprintf("[%s] %s", cat, repo)
		app.Disabled = !selectedMap[label]
	}

	st.Save()

	// 2. Interactive Multi-select for removal
	pterm.Println()
	pterm.Warning.Println("SPACE selects for DELETION. ENTER confirms selection.")
	toDelete, _ := pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultText("Select apps to REMOVE from state completely").
		WithFilter(false).
		Show()

	if len(toDelete) > 0 {
		for _, label := range toDelete {
			// reverse extract repo name
			parts := strings.SplitN(label, "] ", 2)
			if len(parts) == 2 {
				repo := parts[1]
				delete(st.Apps, repo)
				log.Info().Msgf("Removed %s from state.", repo)
			}
		}
		st.Save()
	}

	return nil
}
