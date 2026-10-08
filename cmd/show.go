package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

type ghRestClient interface {
	Get(path string, response interface{}) error
}

var defaultRestClient = func() (ghRestClient, error) {
	return api.DefaultRESTClient()
}

type ReleaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Release struct {
	ID         int64          `json:"id"`
	TagName    string         `json:"tag_name"`
	Name       string         `json:"name"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []ReleaseAsset `json:"assets"`
}

func ShowInfo(r *RootCLI) error {
	client, err := defaultRestClient()
	if err != nil {
		return fmt.Errorf("could not init GitHub REST client: %w", err)
	}
	return showInfoWithClient(r, client)
}

func showInfoWithClient(r *RootCLI, client ghRestClient) error {
	repo := r.Repository
	if repo == "" {
		return fmt.Errorf("repository must be provided")
	}

	var installedVersion string
	var appPinned bool
	if st, err := state.LoadState(); err == nil && st != nil && st.Apps != nil {
		if app, ok := st.Apps[repo]; ok && app != nil {
			installedVersion = app.Version
			appPinned = app.Pinned
		} else {
			for k, app := range st.Apps {
				if strings.EqualFold(k, repo) && app != nil {
					installedVersion = app.Version
					appPinned = app.Pinned
					break
				}
			}
		}
	}

	cfg := loadConfig()
	var disableIcons bool
	if cfg != nil {
		disableIcons = cfg.Core.DisableIcons
		if cfg.Core.NoEmojis {
			r.NoEmojis = true
		}
		if cfg.Core.NoColor {
			r.NoColor = true
		}
	}
	if r.DisableIcons || r.NoEmojis || r.NoColor {
		disableIcons = true
	}
	if r.NoColor {
		ApplyDisplayPreferences(true, false)
	}

	var releases []Release
	if err := client.Get("repos/"+repo+"/releases", &releases); err != nil {
		return fmt.Errorf("failed to fetch releases for %s: %w", repo, err)
	}
	if len(releases) == 0 {
		fmt.Printf("No releases found for %s\n", repo)
		return nil
	}

	// Sort releases by version in descending order
	sort.Slice(releases, func(i, j int) bool {
		return compareVersions(releases[i].TagName, releases[j].TagName) > 0
	})

	var latestStable *Release
	var latestPrerelease *Release
	for i := range releases {
		if releases[i].Draft {
			continue
		}
		if !releases[i].Prerelease && latestStable == nil {
			latestStable = &releases[i]
		}
		if releases[i].Prerelease && latestPrerelease == nil {
			latestPrerelease = &releases[i]
		}
	}

	formatRelease := func(rel *Release, installedVer string, pinned bool, latestStableRel *Release, latestPrereleaseRel *Release, noIcons bool) string {
		isInstalled := installedVer != "" && (rel.TagName == installedVer || strings.TrimPrefix(rel.TagName, "v") == strings.TrimPrefix(installedVer, "v"))
		isPinned := isInstalled && pinned
		isLatestP := latestPrereleaseRel != nil && (rel == latestPrereleaseRel || (rel.ID != 0 && rel.ID == latestPrereleaseRel.ID))
		isLatestS := latestStableRel != nil && (rel == latestStableRel || (rel.ID != 0 && rel.ID == latestStableRel.ID))
		indicator := GetStateIndicator(isInstalled, isPinned, rel.Prerelease, isLatestP, isLatestS, noIcons)
		if indicator != "" {
			return indicator + " " + rel.TagName
		}
		return rel.TagName
	}

	pickTargetRelease := func() *Release {
		if r.ReleaseVersion != "" && r.ReleaseVersion != "latest" {
			for i := range releases {
				if releases[i].TagName == r.ReleaseVersion ||
					strings.TrimPrefix(releases[i].TagName, "v") == strings.TrimPrefix(r.ReleaseVersion, "v") {
					return &releases[i]
				}
			}
			var single Release
			if err := client.Get(fmt.Sprintf("repos/%s/releases/tags/%s", repo, r.ReleaseVersion), &single); err == nil && single.TagName != "" {
				return &single
			}
			return nil
		}
		if r.Prerelease {
			if latestPrerelease != nil {
				return latestPrerelease
			}
			return latestStable
		}
		if latestStable != nil {
			return latestStable
		}
		if latestPrerelease != nil {
			return latestPrerelease
		}
		if len(releases) > 0 {
			return &releases[0]
		}
		return nil
	}

	fetchAssets := func(target *Release) ([]ReleaseAsset, error) {
		if target == nil {
			return nil, fmt.Errorf("no target release")
		}
		var assets []ReleaseAsset
		var getErr error
		if target.ID != 0 {
			getErr = client.Get(fmt.Sprintf("repos/%s/releases/%d/assets?per_page=100", repo, target.ID), &assets)
		}
		if len(assets) == 0 && len(target.Assets) > 0 {
			return target.Assets, nil
		}
		if getErr != nil && len(assets) == 0 {
			return nil, getErr
		}
		return assets, nil
	}

	showVersionsLimit := r.ShowVersions
	showAssetsLimit := r.ShowAssets
	showDescLimit := r.ShowDescription
	showReadmeLimit := r.ShowReadme

	if showVersionsLimit <= -1 && showAssetsLimit <= -1 && showDescLimit <= -1 && showReadmeLimit <= -1 {
		showVersionsLimit = 10
		showAssetsLimit = 25
		showDescLimit = 10
		showReadmeLimit = 50
	}

	order := getShowFlagOrder()
	if len(order) == 0 {
		if showVersionsLimit > -1 {
			order = append(order, "versions")
		}
		if showAssetsLimit > -1 {
			order = append(order, "assets")
		}
		if showDescLimit > -1 {
			order = append(order, "description")
		}
		if showReadmeLimit > -1 {
			order = append(order, "readme")
		}
	}

	target := pickTargetRelease()
	var assets []ReleaseAsset
	if showAssetsLimit > -1 {
		if target == nil {
			return fmt.Errorf("no release found for %s", repo)
		}
		{
			var err error
			assets, err = fetchAssets(target)
			if err != nil {
				return fmt.Errorf("failed to fetch assets: %w", err)
			}
		}
	}

	for i, section := range order {
		if i > 0 {
			fmt.Println()
		}
		switch section {
		case "versions":
			if showVersionsLimit <= -1 {
				continue
			}
			if disableIcons {
				fmt.Println("--- VERSIONS ---")
			} else {
				fmt.Println("--- 📦 VERSIONS 📦 ---")
			}
			limit := len(releases)
			if showVersionsLimit > -1 && limit > showVersionsLimit {
				limit = showVersionsLimit
			}
			var versionItems []string
			for j := 0; j < limit; j++ {
				versionItems = append(versionItems, formatRelease(&releases[j], installedVersion, appPinned, latestStable, latestPrerelease, disableIcons))
			}
			printColumns(versionItems, 4)
			if len(releases) > limit {
				fmt.Println("...")
			}
		case "assets":
			if showAssetsLimit <= -1 {
				continue
			}
			if disableIcons {
				fmt.Println("--- ASSETS ---")
			} else {
				fmt.Println("--- 📂 ASSETS 📂 ---")
			}
			classifier := selector.NewAssetClassifier(runtime.GOOS, runtime.GOARCH, r.Wine, "")
			var assetNames []string
			var assetSizes []int64
			for _, a := range assets {
				assetNames = append(assetNames, a.Name)
				assetSizes = append(assetSizes, a.Size)
			}

			types := r.Type
			if len(types) == 0 {
				if cfg != nil && cfg.Core.InstallTypes != "" {
					for _, t := range strings.Split(cfg.Core.InstallTypes, ",") {
						t = strings.TrimSpace(t)
						if t != "" {
							types = append(types, t)
						}
					}
				}
			}

			regexps := r.ReleaseAssetRegexps
			if len(regexps) == 0 {
				if r.ReleaseAssetRegexp != "" {
					regexps = []string{r.ReleaseAssetRegexp}
				} else {
					regexps = buildRegexFromTypes(types, r.Wine)
				}
			}

			classifiedMap := classifier.ClassifyReleaseWithContext(repo, assetNames, assetSizes, regexps)

			type categoryDef struct {
				cat   selector.InstallCategory
				title string
			}
			catDefs := []categoryDef{
				{selector.CategoryDefaultInstall, "Will be installed by default"},
				{selector.CategorySidecarInstall, "Will be installed with -s (--include-sidecars)"},
				{selector.CategoryInstallableAlternative, "Can be installed but will not be"},
				{selector.CategoryCannotInstall, "Should not / cannot be installed"},
			}

			printedCount := 0
			truncated := false

			for _, catDef := range catDefs {
				items := classifiedMap[catDef.cat]
				if len(items) == 0 {
					continue
				}

				if showAssetsLimit > -1 && printedCount >= showAssetsLimit {
					truncated = true
					break
				}

				subcatMap := make(map[selector.ArchSubcategory][]selector.ClassifiedAsset)
				for _, it := range items {
					subcatMap[it.Subcat] = append(subcatMap[it.Subcat], it)
				}

				catHeaderPrinted := false

				for _, subcat := range classifier.SubcategoryOrder() {
					subItems := subcatMap[subcat]
					if len(subItems) == 0 {
						continue
					}
					if showAssetsLimit > -1 && printedCount >= showAssetsLimit {
						truncated = true
						break
					}

					if !catHeaderPrinted {
						if disableIcons || r.NoColor {
							fmt.Printf("• %s:\n", catDef.title)
						} else {
							fmt.Println(pterm.NewStyle(pterm.Bold, pterm.FgLightWhite).Sprintf("• %s:", catDef.title))
						}
						catHeaderPrinted = true
					}

					fmt.Println(classifier.FormatSubcategoryHeader(subcat, r.NoColor))
					for _, item := range subItems {
						if showAssetsLimit > -1 && printedCount >= showAssetsLimit {
							truncated = true
							break
						}
						osIcon := classifier.FormatOS(item.OS, disableIcons)
						fmt.Printf("    %s %s\n", osIcon, item.Name)
						printedCount++
					}
				}
			}

			if truncated || (showAssetsLimit > -1 && len(assets) > showAssetsLimit) {
				fmt.Println("...")
			}
		case "description":
			if showDescLimit <= -1 {
				continue
			}
			if disableIcons {
				fmt.Println("--- DESCRIPTION ---")
			} else {
				fmt.Println("--- 📝 DESCRIPTION 📝 ---")
			}

			var repoInfo struct {
				Description string `json:"description"`
			}
			err := client.Get("repos/"+repo, &repoInfo)
			if err == nil && repoInfo.Description != "" {
				lines := strings.Split(repoInfo.Description, "\n")
				limit := len(lines)
				if showDescLimit > -1 && limit > showDescLimit {
					limit = showDescLimit
				}
				for j := 0; j < limit; j++ {
					fmt.Println(lines[j])
				}
				if len(lines) > limit {
					fmt.Println("...")
				}
			}
		case "readme":
			if showReadmeLimit <= -1 {
				continue
			}
			if disableIcons {
				fmt.Println("--- README ---")
			} else {
				fmt.Println("--- 📖 README 📖 ---")
			}

			var readme struct {
				Content string `json:"content"`
			}
			err := client.Get("repos/"+repo+"/readme", &readme)
			if err == nil && readme.Content != "" {
				decoded, decErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(readme.Content, "\n", ""))
				content := readme.Content
				if decErr == nil {
					content = string(decoded)
				}
				rendered, renderErr := glamour.Render(content, "dark")
				if renderErr == nil {
					content = rendered
				}
				lines := strings.Split(content, "\n")
				limit := len(lines)
				if showReadmeLimit > -1 && limit > showReadmeLimit {
					limit = showReadmeLimit
				}
				for j := 0; j < limit; j++ {
					fmt.Println(lines[j])
				}
				if len(lines) > limit {
					fmt.Println("...")
				}
			}
		}
	}

	if r.DiscoverSidecars {
		discoverSidecars(client, repo, r.ReleaseVersion)
	}

	return nil
}

func discoverSidecars(client ghRestClient, repo, version string) {
	type treeItem struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	type treeResponse struct {
		Tree []treeItem `json:"tree"`
	}

	ref := version
	if ref == "" || ref == "latest" {
		ref = "HEAD"
	}

	var tree treeResponse
	if err := client.Get(fmt.Sprintf("repos/%s/git/trees/%s?recursive=1", repo, ref), &tree); err != nil {
		fmt.Printf("Failed to discover sidecars: %v\n", err)
		return
	}

	sidecarExts := []string{".so", ".dll", ".dylib", ".red", ".pak", ".json", ".yaml", ".yml"}
	var sidecars []string
	for _, item := range tree.Tree {
		if item.Type != "blob" {
			continue
		}
		for _, ext := range sidecarExts {
			if strings.HasSuffix(strings.ToLower(item.Path), ext) {
				sidecars = append(sidecars, item.Path)
				break
			}
		}
	}

	if len(sidecars) == 0 {
		fmt.Println("\nNo potential sidecar assets found.")
		return
	}

	fmt.Println("\nPotential Sidecar Assets:")
	patterns := suggestSidecarPatterns(sidecars)
	for _, p := range patterns {
		fmt.Printf("  --sidecars '%s'\n", p)
	}
}

func suggestSidecarPatterns(paths []string) []string {
	dirExtMap := make(map[string]map[string]bool)
	for _, p := range paths {
		dir := filepath.Dir(p)
		ext := filepath.Ext(p)
		if dirExtMap[dir] == nil {
			dirExtMap[dir] = make(map[string]bool)
		}
		dirExtMap[dir][ext] = true
	}

	var patterns []string
	for dir, exts := range dirExtMap {
		for ext := range exts {
			pattern := filepath.Join(dir, "*"+ext)
			if dir == "." {
				pattern = "*" + ext
			}
			patterns = append(patterns, pattern)
		}
	}
	sort.Strings(patterns)
	return patterns
}

func printColumns(items []string, maxCols int) {
	if len(items) == 0 {
		return
	}
	termWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termWidth = w
	}

	cols := maxCols
	if cols < 1 {
		cols = 1
	}
	for cols > 1 {
		wrapped := 0
		colWidth := (termWidth - (cols-1)*2) / cols
		if colWidth < 1 {
			cols--
			continue
		}
		for _, item := range items {
			if runewidth.StringWidth(item) > colWidth {
				wrapped++
			}
		}
		if wrapped*2 <= len(items) {
			break
		}
		cols--
	}

	colWidth := (termWidth - (cols-1)*2) / cols
	if colWidth < 1 {
		colWidth = 1
	}

	for i, item := range items {
		w := runewidth.StringWidth(item)
		padding := colWidth - w
		if padding < 0 {
			padding = 0
		}
		if (i+1)%cols == 0 || i == len(items)-1 {
			fmt.Println(item)
		} else {
			fmt.Printf("%s%s  ", item, strings.Repeat(" ", padding))
		}
	}
}

// compareVersions compares two version strings and returns:
// -1 if a < b, 0 if a == b, 1 if a > b
// Handles versions like "v1.2.3", "1.2.3", "v1.2.3-preview.4"
func compareVersions(a, b string) int {
	// Remove 'v' prefix if present
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	// Split into main version and prerelease
	aParts := strings.SplitN(a, "-", 2)
	bParts := strings.SplitN(b, "-", 2)

	aMain := aParts[0]
	bMain := bParts[0]
	aPre := ""
	bPre := ""
	if len(aParts) > 1 {
		aPre = aParts[1]
	}
	if len(bParts) > 1 {
		bPre = bParts[1]
	}

	// Compare main version numbers
	aNums := strings.Split(aMain, ".")
	bNums := strings.Split(bMain, ".")

	maxLen := len(aNums)
	if len(bNums) > maxLen {
		maxLen = len(bNums)
	}

	for i := 0; i < maxLen; i++ {
		var aVal, bVal int
		if i < len(aNums) {
			aVal, _ = strconv.Atoi(aNums[i])
		}
		if i < len(bNums) {
			bVal, _ = strconv.Atoi(bNums[i])
		}

		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
	}

	// Main versions are equal, compare prerelease
	// No prerelease > prerelease (1.0.0 > 1.0.0-preview)
	if aPre == "" && bPre != "" {
		return 1
	}
	if aPre != "" && bPre == "" {
		return -1
	}
	if aPre == "" && bPre == "" {
		return 0
	}

	// Both have prerelease, compare them lexicographically
	if aPre < bPre {
		return -1
	}
	if aPre > bPre {
		return 1
	}
	return 0
}
