package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/log"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
)

var execCommand = exec.Command

func fixEmojiPadding(data pterm.TableData) pterm.TableData {
	for i, row := range data {
		for j, cell := range row {
			displayW := runewidth.StringWidth(cell)
			runeCount := utf8.RuneCountInString(cell)
			if extra := displayW - runeCount; extra > 0 {
				data[i][j] = cell + strings.Repeat(" ", extra)
			}
		}
	}
	return data
}

// extractorDisplay renders the stored extractor precedence using the names of
// the tools actually invoked: "native" shells out to tar/unzip/7z and
// "internal" is the Go archiver library.
func extractorDisplay(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		switch strings.TrimSpace(p) {
		case "native":
			parts[i] = "7z/tar/zip"
		case "internal":
			parts[i] = "go"
		}
	}
	return strings.Join(parts, ",")
}

// wrapCell splits s into lines of at most width runes, preferring to break
// after a natural separator (space, comma, slash, dash, underscore, dot).
func wrapCell(s string, width int) string {
	rs := []rune(s)
	if width <= 0 || len(rs) <= width {
		return s
	}
	var lines []string
	for len(rs) > width {
		cut := width
		for i := width; i > width/2; i-- {
			if strings.ContainsRune(" ,/-_.", rs[i-1]) {
				cut = i
				break
			}
		}
		lines = append(lines, strings.TrimRight(string(rs[:cut]), " "))
		rs = []rune(strings.TrimLeft(string(rs[cut:]), " "))
	}
	if len(rs) > 0 {
		lines = append(lines, string(rs))
	}
	return strings.Join(lines, "\n")
}

func ListState(rList ...*RootCLI) error {
	var r *RootCLI
	if len(rList) > 0 && rList[0] != nil {
		r = rList[0]
	} else {
		r = &RootCLI{}
	}

	st, err := state.LoadState()
	if err != nil {
		return err
	}

	if len(st.Apps) == 0 {
		pterm.Info.Println("No applications are currently managed by gh-pt.")
		return nil
	}

	filterVal := r.Ls
	if r.Ll != "" && r.Ll != "false" {
		filterVal = r.Ll
	}

	var headers []string
	if r.Full {
		headers = []string{"Repository", "Version", "Type", "Scope", "Auto-Update", "Target Path", "Binaries", "Asset", "Symlink", "Extractor", "Script", "Sidecars", "Last VT Scan"}
	} else {
		headers = []string{"Repository", "Version", "Type", "Scope", "Auto-Update", "Target Path", "Helper Script"}
	}

	tableData := pterm.TableData{headers}

	var repos []string
	for k := range st.Apps {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		app := st.Apps[repo]

		// Apply filters
		if filterVal != "" && filterVal != "true" && filterVal != "false" && filterVal != "*" {
			if !strings.Contains(repo, filterVal) && !strings.Contains(strings.Join(app.AssetBinaries, " "), filterVal) {
				continue
			}
		}
		if r.Global && !app.Global {
			continue
		}
		if r.Pin != "" && !app.Pinned {
			continue
		}
		if r.Prerelease && !app.IsPrerelease {
			continue
		}
		if r.Stable && app.IsPrerelease {
			continue
		}

		scope := "User"
		if app.Global {
			scope = "Global"
		}
		autoUpdate := "Enabled"
		if app.Disabled {
			autoUpdate = "Disabled"
		}

		versionDisplay := app.Version
		typeDisplay := strings.Join(app.Type, ",")
		if len(typeDisplay) > 25 || typeDisplay == "" {
			if len(app.PackageNames) > 0 {
				typeDisplay = "package"
			} else {
				typeDisplay = "binary/archive"
			}
		}
		if app.Clone {
			versionDisplay, typeDisplay = "git (clone)", "repo sync"
		} else if app.Fork {
			versionDisplay, typeDisplay = "git (fork)", "repo sync"
		} else if app.CompileScript != "" {
			versionDisplay, typeDisplay = "source (ai script)", "compile-from-source"
		}

		isInstalled := false
		if app.TargetPath != "" {
			if _, err := os.Stat(app.TargetPath); err == nil {
				isInstalled = true
			}
		}
		displayRepo := repo
		if indicator := GetStateIndicator(isInstalled, app.Pinned, app.IsPrerelease, false, false, r.DisableIcons || r.NoEmojis); indicator != "" {
			displayRepo = indicator + " " + repo
		}

		if !r.Full {
			helperScript := app.CompileScript
			if helperScript == "" {
				helperScript = "N/A"
			}
			tableData = append(tableData, []string{
				displayRepo, versionDisplay, typeDisplay, scope, autoUpdate, app.TargetPath, helperScript,
			})
			continue
		}

		binaries := app.InstalledBinaries
		if len(binaries) == 0 {
			binaries = app.PackageNames
		}
		if len(binaries) == 0 {
			binaries = app.AssetBinaries
		}
		asset := app.ReleaseAsset
		if asset == "" {
			asset = strings.Join(app.InstalledAssetsFullNames, ", ")
		}
		tableData = append(tableData, []string{
			displayRepo,
			versionDisplay,
			typeDisplay,
			scope,
			autoUpdate,
			app.TargetPath,
			strings.Join(binaries, ", "),
			asset,
			app.SymlinkDir,
			extractorDisplay(app.Extractor),
			app.CompileScript,
			strings.Join(app.InstalledSidecars, ", "),
			app.LastVTScan,
		})
	}

	if r.Full {
		tableData = dropEmptyColumns(tableData)
	}
	return renderState(tableData, r.ListFormat)
}

// dropEmptyColumns removes columns whose cells are empty in every data row,
// and renders remaining empty cells as "-".
func dropEmptyColumns(data pterm.TableData) pterm.TableData {
	if len(data) < 2 {
		return data
	}
	keep := make([]bool, len(data[0]))
	for _, row := range data[1:] {
		for j, cell := range row {
			if cell != "" {
				keep[j] = true
			}
		}
	}
	out := make(pterm.TableData, len(data))
	for i, row := range data {
		for j, cell := range row {
			if !keep[j] {
				continue
			}
			if i > 0 && cell == "" {
				cell = "-"
			}
			out[i] = append(out[i], cell)
		}
	}
	return out
}

func findTargetApps(st *state.State, target string) []string {
	targetLower := strings.ToLower(target)
	var matches []string

	for repo, app := range st.Apps {
		if strings.ToLower(repo) == targetLower {
			matches = append(matches, repo)
			continue
		}
		for _, renamed := range app.Rename {
			if strings.ToLower(renamed) == targetLower {
				matches = append(matches, repo)
				break
			}
		}
		parts := strings.Split(repo, "/")
		if strings.ToLower(parts[len(parts)-1]) == targetLower {
			matches = append(matches, repo)
		}
	}
	return matches
}

func safeDeletePath(baseDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty target name")
	}
	if filepath.IsAbs(name) || name == "." || name == ".." {
		return "", fmt.Errorf("unsafe delete target %q", name)
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || cleanName == string(filepath.Separator) {
		return "", fmt.Errorf("unsafe delete target %q", name)
	}
	if filepath.Base(cleanName) != cleanName {
		return "", fmt.Errorf("refusing to delete path with traversal %q", name)
	}
	fullPath := filepath.Join(baseDir, cleanName)
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absBase, absFull)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to delete outside target path %q", name)
	}
	return fullPath, nil
}

func RmStateOnly(target string) error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	toRemove := findTargetApps(st, target)
	if len(toRemove) == 0 {
		log.Warnf("No application found matching '%s'", target)
		return nil
	}

	for _, r := range toRemove {
		delete(st.Apps, r)
		log.Infof("Removed %s from state tracking only.", r)
		state.LogHistory("remove", r, "")
	}
	return st.Save()
}
func RemoveApp(target string, purge bool) error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	toRemove := findTargetApps(st, target)
	if len(toRemove) == 0 {
		log.Warnf("No application found matching '%s'", target)
		return nil
	}

	for _, r := range toRemove {
		app := st.Apps[r]

		if app != nil && app.Hooks != nil {
			if script, ok := app.Hooks["pre-uninstall"]; ok && strings.TrimSpace(script) != "" {
				if err := executeHookScript("pre-uninstall", r, script); err != nil {
					return fmt.Errorf("pre-uninstall hook failed for %s: %w", r, err)
				}
			}
		}

		if len(app.PackageNames) > 0 {
			for _, pkgName := range app.PackageNames {
				log.Infof("Uninstalling package %s...", pkgName)
				var cmd *exec.Cmd
				if _, err := exec.LookPath("dpkg"); err == nil {
					cmd = execCommand("sudo", "dpkg", "-r", pkgName)
				} else if _, err := exec.LookPath("rpm"); err == nil {
					cmd = execCommand("sudo", "rpm", "-e", pkgName)
				} else if _, err := exec.LookPath("pacman"); err == nil {
					cmd = execCommand("sudo", "pacman", "-R", "--noconfirm", pkgName)
				} else if _, err := exec.LookPath("pkg"); err == nil {
					cmd = execCommand("sudo", "pkg", "delete", "-y", pkgName)
				}

				if cmd != nil {
					if err := cmd.Run(); err != nil {
						log.Warn(fmt.Sprintf("Failed to uninstall package %s", pkgName), "error", err)
					} else {
						log.Infof("Successfully uninstalled %s", pkgName)
					}
				}
			}
		} else if app.TargetPath != "" && !app.Clone && !app.Fork {
			parts := strings.Split(r, "/")
			repoName := parts[len(parts)-1]

			if len(app.Rename) > 0 {
				for _, renamed := range app.Rename {
					binPath, err := safeDeletePath(app.TargetPath, renamed)
					if err != nil {
						log.Warn(fmt.Sprintf("Skipping unsafe binary name %q", renamed), "error", err)
						continue
					}
					if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
						log.Warn(fmt.Sprintf("Failed to remove binary %s", binPath), "error", err)
					} else if err == nil {
						log.Infof("Deleted %s", binPath)
					}
				}
			} else {
				binPath, err := safeDeletePath(app.TargetPath, repoName)
				if err != nil {
					log.Warn(fmt.Sprintf("Skipping unsafe binary name %q", repoName), "error", err)
				} else {
					if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
						log.Warn(fmt.Sprintf("Failed to remove binary %s", binPath), "error", err)
					} else if err == nil {
						log.Infof("Deleted %s", binPath)
					}
				}
			}

			for _, binName := range app.AssetBinaries {
				binPath, err := safeDeletePath(app.TargetPath, binName)
				if err != nil {
					log.Warn(fmt.Sprintf("Skipping unsafe binary name %q", binName), "error", err)
					continue
				}
				if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
					log.Warn(fmt.Sprintf("Failed to delete %s", binPath), "error", err)
				} else if err == nil {
					log.Infof("Deleted %s", binPath)
				}
			}
		}

		// Sidecar cleanup is now handled by the IncludeSidecars mode logic
		// Sidecars are stored in standard locations (XDG data home, bin, etc.)

		if app.Clone || app.Fork {
			if purge && app.TargetPath != "" {
				if err := os.RemoveAll(app.TargetPath); err != nil {
					log.Warn(fmt.Sprintf("Failed to purge repository directory %s", app.TargetPath), "error", err)
				} else {
					log.Infof("Purged cloned/forked repository at %s", app.TargetPath)
				}
			} else {
				log.Infof("Kept repository directory at %s (use --purge to delete)", app.TargetPath)
			}
		}

		if app.CompileScript != "" {
			if err := os.Remove(app.CompileScript); err != nil && !os.IsNotExist(err) {
				log.Warn(fmt.Sprintf("Failed to remove compile script %s", app.CompileScript), "error", err)
			} else if err == nil {
				if purge {
					log.Infof("Purged compile script %s", app.CompileScript)
				} else {
					log.Infof("Removed compile script %s", app.CompileScript)
				}
			}

			repoParts := strings.Split(r, "/")
			if len(repoParts) == 2 {
				srcPath := filepath.Join(os.TempDir(), "gh-pt-src-"+repoParts[1])
				_ = os.RemoveAll(srcPath)
			}
		}

		delete(st.Apps, r)
		log.Infof("Removed %s from state tracking.", r)
		state.LogHistory("remove", r, "")
	}
	return st.Save()
}

func PinAppState(target string) error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	toPin := findTargetApps(st, target)
	if len(toPin) == 0 {
		log.Warnf("No application found matching '%s'", target)
		return nil
	}

	for _, r := range toPin {
		app := st.Apps[r]
		app.Pinned = true
		log.Infof("Pinned %s in state.", r)
	}
	return st.Save()
}

func EditState() error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	if len(st.Apps) == 0 {
		pterm.Info.Println("No applications are currently managed by gh-pt.")
		return nil
	}

	for {
		action, _ := pterm.DefaultInteractiveSelect.
			WithOptions([]string{"Toggle Auto-Updates (Batch)", "Remove Apps (Batch)", "Edit App Settings", "Exit"}).
			WithDefaultText("Select State Management Action").
			Show()

		switch action {
		case "Toggle Auto-Updates (Batch)":
			editStateToggleUpdates(st)
		case "Remove Apps (Batch)":
			editStateRemoveApps(st)
		case "Edit App Settings":
			editStateAppFields(st)
		case "Exit":
			return nil
		}
	}
}

func editStateToggleUpdates(st *state.State) {
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

	if err := st.Save(); err != nil {
		pterm.Error.Printf("Failed to save state: %v\n", err)
	} else {
		pterm.Success.Println("State saved successfully.")
	}
}

func editStateRemoveApps(st *state.State) {
	var options []string
	for k, app := range st.Apps {
		cat := "User"
		if app.Global {
			cat = "Global"
		}
		options = append(options, fmt.Sprintf("[%s] %s", cat, k))
	}
	sort.Strings(options)

	pterm.Warning.Println("SPACE selects for DELETION. ENTER confirms selection.")
	toDelete, _ := pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithDefaultText("Select apps to REMOVE from state completely").
		WithFilter(false).
		Show()

	if len(toDelete) > 0 {
		for _, label := range toDelete {
			parts := strings.SplitN(label, "] ", 2)
			if len(parts) == 2 {
				repo := parts[1]
				delete(st.Apps, repo)
				log.Infof("Removed %s from state.", repo)
				state.LogHistory("remove", repo, "")
			}
		}
		if err := st.Save(); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
		} else {
			pterm.Success.Println("State saved successfully.")
		}
	}
}

func editStateAppFields(st *state.State) {
	var repos []string
	for k := range st.Apps {
		repos = append(repos, k)
	}
	sort.Strings(repos)
	repos = append(repos, "Back")

	selectedAppRepo, _ := pterm.DefaultInteractiveSelect.
		WithOptions(repos).
		WithDefaultText("Select App to Edit").
		Show()

	if selectedAppRepo == "Back" {
		return
	}

	app := st.Apps[selectedAppRepo]
	for {
		fields := []string{
			fmt.Sprintf("TargetPath: %s", app.TargetPath),
			fmt.Sprintf("Global: %t", app.Global),
			fmt.Sprintf("ReleaseAsset: %s", app.ReleaseAsset),
			fmt.Sprintf("Version: %s", app.Version),
			fmt.Sprintf("Disabled: %t", app.Disabled),
			fmt.Sprintf("Extractor: %s", app.Extractor),
			fmt.Sprintf("IsPrerelease: %t", app.IsPrerelease),
			"Back",
		}

		selectedField, _ := pterm.DefaultInteractiveSelect.
			WithOptions(fields).
			WithDefaultText("Select Field to Edit").
			Show()

		if selectedField == "Back" {
			break
		}

		parts := strings.Split(selectedField, ":")
		if len(parts) == 0 {
			continue
		}
		fieldName := strings.TrimSpace(parts[0])

		switch fieldName {
		case "TargetPath":
			app.TargetPath, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("TargetPath").WithDefaultValue(app.TargetPath).Show()
		case "ReleaseAsset":
			app.ReleaseAsset, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("ReleaseAsset").WithDefaultValue(app.ReleaseAsset).Show()
		case "Version":
			app.Version, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("Version").WithDefaultValue(app.Version).Show()
		case "Global":
			app.Global, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("Global").WithDefaultValue(app.Global).Show()
		case "Disabled":
			app.Disabled, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("Disabled").WithDefaultValue(app.Disabled).Show()
		case "Extractor":
			app.Extractor, _ = pterm.DefaultInteractiveTextInput.WithDefaultText("Extractor").WithDefaultValue(app.Extractor).Show()
		case "IsPrerelease":
			app.IsPrerelease, _ = pterm.DefaultInteractiveConfirm.WithDefaultText("IsPrerelease").WithDefaultValue(app.IsPrerelease).Show()
		}
	}

	if err := st.Save(); err != nil {
		pterm.Error.Printf("Failed to save state: %v\n", err)
	} else {
		pterm.Success.Println("App state updated successfully.")
	}
}
