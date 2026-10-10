package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/posener/complete"
)

// ConfigKeys holds the hardcoded configuration setting keys corresponding to config.yml
var ConfigKeys = []string{
	"add_deps",
	"ai_cmd",
	"ai_interactive_cmd",
	"allow_prerelease",
	"avx_level",
	"clone_path",
	"disable_icons",
	"disable_prompts",
	"extractor",
	"fork_path",
	"global",
	"global_path",
	"install_path",
	"install_types",
	"keep_suffixes",
	"log_to_file",
	"no_color",
	"no_deps",
	"no_emojis",
	"no_save_state",
	"package_path",
	"progress_bar",
	"repo_dir",
	"resolve_deps",
	"sidecar_path",
	"symlink",
	"target_base_dir",
	"vt_api_key",
	"wine",
}

func predictInstalledApps(args complete.Args) []string {
	st, err := state.LoadState()
	if err != nil || st == nil || st.Apps == nil {
		return []string{}
	}
	keys := make([]string, 0, len(st.Apps))
	for repo := range st.Apps {
		if repo != "" {
			keys = append(keys, repo)
		}
	}
	sort.Strings(keys)
	return keys
}

func predictGithubRepos(args complete.Args) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !strings.Contains(args.Last, "/") {
		if len(args.Last) < 2 {
			return []string{}
		}
		q := args.Last
		// Search for users/orgs matching $ownerid prefix
		cmd := exec.CommandContext(ctx, "gh", "api", "-X", "GET", "search/users", "-f", "q="+q+" in:login", "-f", "per_page=15", "-q", ".items[].login")
		out, err := cmd.Output()
		if err == nil {
			scanner := bufio.NewScanner(bytes.NewReader(out))
			var owners []string
			for scanner.Scan() {
				login := strings.TrimSpace(scanner.Text())
				if login != "" {
					owners = append(owners, login)
				}
			}
			if len(owners) > 0 {
				return owners
			}
		}

		// Fallback to searching repositories
		cmdRepo := exec.CommandContext(ctx, "gh", "api", "-X", "GET", "search/repositories", "-f", "q="+q, "-f", "per_page=15", "-q", ".items[].full_name")
		outRepo, errRepo := cmdRepo.Output()
		if errRepo != nil {
			return []string{}
		}

		scanner := bufio.NewScanner(bytes.NewReader(outRepo))
		var repos []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				repos = append(repos, line)
			}
		}
		return repos
	}

	parts := strings.SplitN(args.Last, "/", 2)
	owner := parts[0]
	repoPrefix := parts[1]
	if owner == "" {
		return []string{}
	}

	searchQuery := fmt.Sprintf("user:%s", owner)
	if repoPrefix != "" {
		searchQuery = fmt.Sprintf("user:%s %s in:name", owner, repoPrefix)
	}

	cmd := exec.CommandContext(ctx, "gh", "api", "-X", "GET", "search/repositories", "-f", "q="+searchQuery, "-f", "per_page=100", "-q", ".items[].full_name")
	out, err := cmd.Output()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(out))
		var repos []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				repos = append(repos, line)
			}
		}
		if len(repos) > 0 {
			return repos
		}
	}

	// Fallback to legacy user repos API endpoint
	cmdLegacy := exec.CommandContext(ctx, "gh", "api", fmt.Sprintf("users/%s/repos", owner), "-f", "per_page=100", "-q", ".[].full_name")
	outLegacy, errLegacy := cmdLegacy.Output()
	if errLegacy != nil {
		return []string{}
	}

	scannerLegacy := bufio.NewScanner(bytes.NewReader(outLegacy))
	var repos []string
	for scannerLegacy.Scan() {
		line := strings.TrimSpace(scannerLegacy.Text())
		if line != "" {
			repos = append(repos, line)
		}
	}
	return repos
}

func predictConfigKeys(args complete.Args) []string {
	return ConfigKeys
}

var (
	PredictInstalledApps complete.Predictor = complete.PredictFunc(predictInstalledApps)
	PredictGithubRepos   complete.Predictor = complete.PredictFunc(predictGithubRepos)
	PredictConfigKeys    complete.Predictor = complete.PredictFunc(predictConfigKeys)
	PredictExtractors    complete.Predictor = complete.PredictSet("default", "ouch", "native", "internal")
	PredictProgressBars  complete.Predictor = complete.PredictSet("pacman", "standard", "conveyor", "none", "spinner:dots", "spinner:line", "spinner:jump", "spinner:pulse", "spinner:points", "spinner:miniDot", "spinner:step")
	PredictSidecarModes  complete.Predictor = complete.PredictSet("auto", "xdg_data_home", "local-map", "bin", "none")

	InstalledAppsPredictor = PredictInstalledApps
	GithubReposPredictor   = PredictGithubRepos
	ConfigKeysPredictor    = PredictConfigKeys
)

func NewInstalledAppsPredictor() complete.Predictor { return PredictInstalledApps }
func NewGithubReposPredictor() complete.Predictor   { return PredictGithubRepos }
func NewConfigKeysPredictor() complete.Predictor    { return PredictConfigKeys }
func NewExtractorsPredictor() complete.Predictor    { return PredictExtractors }
func NewProgressBarsPredictor() complete.Predictor  { return PredictProgressBars }
func NewSidecarModesPredictor() complete.Predictor  { return PredictSidecarModes }
