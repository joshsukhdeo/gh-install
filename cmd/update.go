package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/release"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/pterm/pterm"
	"golang.org/x/sync/errgroup"
)

// GQLClient defines the interface for sending GraphQL requests.
type GQLClient interface {
	Do(query string, variables map[string]interface{}, response interface{}) error
}

var newGraphQLClient = func() (GQLClient, error) {
	return api.DefaultGraphQLClient()
}

var installReleaseFunc = func(appParams *params.ExecContext, ghClient *api.RESTClient) error {
	installRelease := release.MakeGithubRelease(appParams, ghClient)
	return installRelease.Install()
}

var UpdateConcurrencyLimit = 4

// GQLRepoResult represents the GraphQL response for a single repository release.
type GQLRepoResult struct {
	LatestRelease *struct {
		TagName string `json:"tagName"`
		Name    string `json:"name"`
	} `json:"latestRelease"`
}

// BuildBatchReleaseQuery constructs a single dynamic GraphQL query with aliases
// to fetch the latest release tag for every repository in repos.
func BuildBatchReleaseQuery(repos []string) (string, map[string]string) {
	aliasToRepo := make(map[string]string)
	if len(repos) == 0 {
		return "", aliasToRepo
	}

	repoSet := make(map[string]bool)
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo != "" {
			parts := strings.Split(repo, "/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				repoSet[repo] = true
			}
		}
	}

	if len(repoSet) == 0 {
		return "", aliasToRepo
	}

	sortedRepos := make([]string, 0, len(repoSet))
	for r := range repoSet {
		sortedRepos = append(sortedRepos, r)
	}
	sort.Strings(sortedRepos)

	var sb strings.Builder
	sb.WriteString("query {\n")
	for i, repo := range sortedRepos {
		parts := strings.Split(repo, "/")
		owner := parts[0]
		name := parts[1]
		alias := fmt.Sprintf("repo_%d", i)
		aliasToRepo[alias] = repo
		fmt.Fprintf(&sb, "  %s: repository(owner: %s, name: %s) {\n    latestRelease {\n      tagName\n    }\n  }\n", alias, strconv.Quote(owner), strconv.Quote(name))
	}
	sb.WriteString("}\n")

	return sb.String(), aliasToRepo
}

const maxGraphQLBatchSize = 30

// FetchLatestReleaseTags sends batched GraphQL queries to GitHub in chunks of 30
// and returns a map of repository name to latest release tag.
func FetchLatestReleaseTags(client GQLClient, repos []string) (map[string]string, error) {
	repoSet := make(map[string]bool)
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo != "" {
			parts := strings.Split(repo, "/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				repoSet[repo] = true
			}
		}
	}
	if len(repoSet) == 0 {
		return make(map[string]string), nil
	}

	sortedRepos := make([]string, 0, len(repoSet))
	for r := range repoSet {
		sortedRepos = append(sortedRepos, r)
	}
	sort.Strings(sortedRepos)

	mergedTags := make(map[string]string)
	for i := 0; i < len(sortedRepos); i += maxGraphQLBatchSize {
		end := i + maxGraphQLBatchSize
		if end > len(sortedRepos) {
			end = len(sortedRepos)
		}
		chunk := sortedRepos[i:end]
		query, aliasToRepo := BuildBatchReleaseQuery(chunk)
		if query == "" {
			continue
		}

		var rawResponse map[string]GQLRepoResult
		err := client.Do(query, nil, &rawResponse)
		if err != nil {
			var gqlErr *api.GraphQLError
			if errors.As(err, &gqlErr) && len(rawResponse) > 0 {
				log.Warn("GraphQL batch query returned partial errors", "error", err)
			} else if len(rawResponse) == 0 {
				return nil, fmt.Errorf("failed to fetch latest releases via GraphQL: %w", err)
			}
		}

		for alias, result := range rawResponse {
			repo := aliasToRepo[alias]
			if repo == "" {
				continue
			}
			if result.LatestRelease != nil {
				tag := result.LatestRelease.TagName
				if tag == "" {
					tag = result.LatestRelease.Name
				}
				if tag != "" {
					mergedTags[repo] = tag
				}
			}
		}
	}

	return mergedTags, nil
}

func DoUpdate(r *RootCLI, ghClient *api.RESTClient) error {
	if r.Barbarous {
		r.InsecureAllowUnsigned = true
		r.VerifyChecksum = false
		r.SkipVtSandbox = true
		r.AllowForeignArch = true
		r.AllowDowngrade = true
		if r.Wine == "" || r.Wine == "off" {
			r.Wine = "allow"
		}
	}
	if r.LeRetrogrouch {
		r.AllowDowngrade = true
		r.PinInstall = false
	}
	if r.RetrogradeStopgap {
		r.AllowDowngrade = true
		r.PinInstall = true
	}
	if r.SelfInflictedDebt {
		r.AllowDowngrade = true
	}

	st, err := state.LoadState()
	if err != nil {
		return err
	}

	if len(st.Apps) == 0 {
		log.Info("No tracked applications in state.")
		return nil
	}

	// First handle compile scripts and cloned/forked repos if applicable
	for _, app := range st.Apps {
		specificallyTargeted := (r.Repository != "" && strings.EqualFold(r.Repository, app.Repository))

		if r.Repository != "" && !specificallyTargeted {
			continue
		}

		if app.Disabled && !specificallyTargeted {
			continue
		}

		if app.Pinned && !specificallyTargeted {
			continue
		}

		if r.Update && !r.UpdateAll {
			if r.Global && !app.Global {
				continue
			}
			if !r.Global && app.Global {
				continue
			}
		}

		if app.CompileScript != "" {
			// Check remote commit to see if we need an update
			remoteCommit := ""
			checkCmd := exec.Command("gh", "api", "repos/"+app.Repository+"/commits/HEAD", "--jq", ".sha")
			if out, err := checkCmd.Output(); err == nil {
				remoteCommit = strings.TrimSpace(string(out))
				if remoteCommit != "" && remoteCommit == app.Version {
					log.Info(fmt.Sprintf("Skipping %s (already at latest commit %s)", app.Repository, app.Version))
					continue
				}
				if remoteCommit != "" {
					log.Info(fmt.Sprintf("Updating %s from %s to %s", app.Repository, app.Version, remoteCommit))
				}
			} else {
				log.Warn(fmt.Sprintf("Could not check remote commit for %s, proceeding with update", app.Repository))
			}

			if r.DryRun {
				log.Info(fmt.Sprintf("[dry-run] Would execute compile script %s for %s", app.CompileScript, app.Repository))
				continue
			}

			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", app.CompileScript)
			} else {
				cmd = exec.Command("sh", app.CompileScript)
			}
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				log.Error(fmt.Sprintf("Failed to update %s via compile script", app.Repository), "error", err)
			} else {
				log.Info(fmt.Sprintf("Successfully updated %s via compile script", app.Repository))
				if remoteCommit != "" {
					app.Version = remoteCommit
				}

				state.LogHistory("update", app.Repository, app.Version)
				// Save the updated version to state
				if err := st.AddApp(app); err != nil {
					log.Warn(fmt.Sprintf("could not update state for %s", app.Repository), "error", err)
				}
			}
			continue
		}

		if app.Clone || app.Fork {
			if r.DryRun {
				log.Info(fmt.Sprintf("[dry-run] Would sync repo %s at %s", app.Repository, app.TargetPath))
				continue
			}

			var syncArgs []string
			if app.Fork {
				syncArgs = []string{"repo", "sync", "--source", app.Repository}
			} else {
				syncArgs = []string{"repo", "sync"}
			}

			// Run gh repo sync in the target repository directory
			cmd := exec.Command("gh", syncArgs...)
			cmd.Dir = app.TargetPath
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Error(fmt.Sprintf("Failed to sync %s: %s", app.Repository, string(output)), "error", err)
			} else {
				log.Info(fmt.Sprintf("Successfully synced %s", app.Repository))

				if app.Fork && r.Overwrite {
					pullCmd := exec.Command("git", "pull", "origin", "--force")
					pullCmd.Dir = app.TargetPath
					pullOutput, pullErr := pullCmd.CombinedOutput()
					if pullErr != nil {
						log.Error(fmt.Sprintf("Failed to pull from origin for %s: %s", app.Repository, string(pullOutput)), "error", pullErr)
					} else {
						log.Info(fmt.Sprintf("Successfully pulled from origin for %s", app.Repository))
					}
				}

				state.LogHistory("update", app.Repository, app.Version)
			}
			continue
		}
	}

	// Dynamic GraphQL query using repository aliases to fetch the latest release tag
	// for every repository tracked in state.json in exactly one network round-trip.
	trackedRepos := make([]string, 0, len(st.Apps))
	for repo := range st.Apps {
		if repo == "" {
			continue
		}
		if r.Repository != "" && !strings.EqualFold(r.Repository, repo) {
			continue
		}
		trackedRepos = append(trackedRepos, repo)
	}
	sort.Strings(trackedRepos)

	gqlClient, err := newGraphQLClient()
	if err != nil {
		log.Error("Could not initialize GitHub GraphQL client", "error", err)
		return err
	}

	latestTags, err := FetchLatestReleaseTags(gqlClient, trackedRepos)
	if err != nil {
		log.Error("Failed to fetch latest releases via GraphQL", "error", err)
		return err
	}

	// Compare fetched latest tags against Version in state.InstalledApp.
	// Filter out apps that are already up to date.
	type appUpdateCandidate struct {
		app       *state.InstalledApp
		latestTag string
	}
	var outdatedApps []*appUpdateCandidate

	for _, app := range st.Apps {
		if app.CompileScript != "" || app.Clone || app.Fork {
			continue // handled above
		}

		specificallyTargeted := (r.Repository != "" && strings.EqualFold(r.Repository, app.Repository))

		if r.Repository != "" && !specificallyTargeted {
			continue // specifically targeted another app
		}

		if app.Disabled && !specificallyTargeted {
			continue // skip disabled unless specifically targeted
		}

		if app.Pinned && !specificallyTargeted {
			indicator := GetStateIndicator(true, app.Pinned, app.IsPrerelease, false, false, r.DisableIcons || r.NoEmojis)
			repoDisplay := app.Repository
			if indicator != "" {
				repoDisplay = indicator + " " + app.Repository
			}
			log.Info(fmt.Sprintf("Skipping %s (pinned at %s)", repoDisplay, app.Version))
			continue
		}

		if r.Update && !r.UpdateAll {
			if r.Global && !app.Global {
				continue // wants global only, this is user
			}
			if !r.Global && app.Global {
				continue // wants user only, this is global
			}
		}

		latestTag, hasTag := latestTags[app.Repository]
		if !hasTag || latestTag == "" {
			log.Warn(fmt.Sprintf("No release tag found for %s via GraphQL, skipping", app.Repository))
			continue
		}

		// Compare versions - skip if same version is already installed
		// Normalize by stripping leading 'v' to handle version strings like "v1.2.3" vs "1.2.3"
		normalizedLatest := strings.TrimPrefix(latestTag, "v")
		normalizedStored := strings.TrimPrefix(app.Version, "v")

		if latestTag == app.Version || (normalizedLatest != "" && normalizedLatest == normalizedStored) {
			indicator := GetStateIndicator(true, app.Pinned, app.IsPrerelease, false, false, r.DisableIcons || r.NoEmojis)
			repoDisplay := app.Repository
			if indicator != "" {
				repoDisplay = indicator + " " + app.Repository
			}
			log.Info(fmt.Sprintf("Skipping %s (already at latest version %s)", repoDisplay, app.Version))
			continue
		}

		outdatedApps = append(outdatedApps, &appUpdateCandidate{
			app:       app,
			latestTag: latestTag,
		})
	}

	if len(outdatedApps) == 0 {
		log.Info("All tracked applications are up to date.")
		return nil
	}

	if r.DryRun {
		for _, item := range outdatedApps {
			log.Info(fmt.Sprintf("[dry-run] Would update %s from %s to %s", item.app.Repository, item.app.Version, item.latestTag))
		}
		return nil
	}

	// Spin up a worker pool using errgroup to concurrently download and install updates
	var g errgroup.Group
	limit := UpdateConcurrencyLimit
	if limit <= 0 {
		limit = 4
	}
	g.SetLimit(limit)

	var mu sync.Mutex
	successfulUpdates := make(map[string]string)

	for _, item := range outdatedApps {
		app := item.app
		latestTag := item.latestTag

		indicator := GetStateIndicator(true, app.Pinned, app.IsPrerelease, false, false, r.DisableIcons || r.NoEmojis)
		repoDisplay := app.Repository
		if indicator != "" {
			repoDisplay = indicator + " " + app.Repository
		}
		log.Info(fmt.Sprintf("Updating %s from %s to %s", repoDisplay, app.Version, latestTag))

		g.Go(func() error {
			oldInstalledSidecars := append([]string(nil), app.InstalledSidecars...)
			oldInstalledBinaries := append([]string(nil), app.InstalledBinaries...)
			oldTargetPath := app.TargetPath

			appParams := r.ExecContext
			appParams.Repository = app.Repository
			appParams.TargetPath = app.TargetPath
			appParams.Global = app.Global
			appParams.ReleaseAsset = app.ReleaseAsset
			appParams.ReleaseAssetRegexp = app.ReleaseRegexp
			if appParams.ReleaseAssetRegexp == "" {
				appParams.ReleaseAssetRegexps = buildRegexFromTypes(appParams.Type, appParams.Wine)
				appParams.ReleaseAssetRegexp = strings.Join(appParams.ReleaseAssetRegexps, " | ")
			} else {
				appParams.ReleaseAssetRegexps = strings.Split(app.ReleaseRegexp, " | ")
			}
			appParams.Rename = app.Rename
			if len(app.Type) > 0 {
				appParams.Type = app.Type
			}
			appParams.All = app.All
			appParams.AssetBinaries = app.AssetBinaries
			appParams.AssetBinariesRegexp = app.AssetBinariesRegexp
			appParams.Extractor = app.Extractor
			appParams.Sidecars = app.Sidecars
			appParams.SidecarSymlinkTo = app.SidecarSymlinkTo
			appParams.IncludeSidecars = app.IncludeSidecars
			appParams.SidecarMode = app.SidecarMode
			if app.SymlinkDir != "" {
				appParams.Symlink = true
			}
			appParams.EnvInject = app.EnvInject
			appParams.FallbackReleases = app.FallbackReleases
			appParams.ReleaseVersion = "latest"
			if app.IsPrerelease {
				appParams.Prerelease = true
			}
			if r.Stable {
				appParams.Prerelease = false
				appParams.Stable = true
			}
			appParams.IsUpgradeCmd = true
			appParams.NoSaveState = true // thread safety: don't save per worker
			specificallyTargeted := (r.Repository != "" && strings.EqualFold(r.Repository, app.Repository))
			appParams.SpecificallyTargeted = specificallyTargeted
			appParams.InsecureAllowUnsigned = app.AllowsUnsigned(r.InsecureAllowUnsigned, specificallyTargeted, r.Overwrite)

			err := installReleaseFunc(&appParams, ghClient)
			if err != nil {
				log.Error(fmt.Sprintf("Failed to update %s", app.Repository), "error", err)
				return fmt.Errorf("failed to update %s: %w", app.Repository, err)
			}

			// Reconcile obsolete assets after successful update installation
			updatedSidecars := app.InstalledSidecars
			updatedBinaries := app.InstalledBinaries
			targetPath := oldTargetPath
			if targetPath == "" {
				targetPath = app.TargetPath
			}

			if freshState, err := state.LoadState(); err == nil && freshState.Apps != nil {
				if updatedApp, ok := freshState.Apps[app.Repository]; ok && updatedApp != nil {
					if updatedApp.TargetPath != "" {
						targetPath = updatedApp.TargetPath
					}
					if !stringSlicesEqual(updatedApp.InstalledSidecars, oldInstalledSidecars) ||
						!stringSlicesEqual(updatedApp.InstalledBinaries, oldInstalledBinaries) ||
						updatedApp.Version == latestTag {
						updatedSidecars = updatedApp.InstalledSidecars
						updatedBinaries = updatedApp.InstalledBinaries
					}
				}
			}

			newSidecarSet := make(map[string]bool, len(updatedSidecars)*2)
			for _, sc := range updatedSidecars {
				newSidecarSet[sc] = true
				newSidecarSet[filepath.Clean(sc)] = true
			}

			var removedLdso, removedUdev bool
			for _, oldSidecar := range oldInstalledSidecars {
				if strings.TrimSpace(oldSidecar) == "" {
					continue
				}
				if !newSidecarSet[oldSidecar] && !newSidecarSet[filepath.Clean(oldSidecar)] {
					if _, err := os.Lstat(oldSidecar); err == nil {
						if strings.Contains(oldSidecar, "ld.so.conf.d") {
							removedLdso = true
						}
						if strings.Contains(oldSidecar, "udev/rules.d") {
							removedUdev = true
						}
						if err := os.Remove(oldSidecar); err != nil && !os.IsNotExist(err) {
							if os.IsPermission(err) && strings.HasPrefix(filepath.Clean(oldSidecar), "/etc") {
								cmd := execCommand("sudo", "rm", "-f", oldSidecar)
								if sudoErr := cmd.Run(); sudoErr != nil {
									log.Warn(fmt.Sprintf("Failed to remove obsolete sidecar %s with sudo", oldSidecar), "error", sudoErr)
								} else {
									log.Info(fmt.Sprintf("Removed obsolete sidecar %s with sudo", oldSidecar))
									stopDirs := getPruneStopDirs()
									if targetPath != "" {
										stopDirs = append(stopDirs, targetPath)
									}
									pruneEmptyParentDirs(filepath.Dir(oldSidecar), stopDirs)
								}
							} else {
								log.Warn(fmt.Sprintf("Failed to remove obsolete sidecar %s", oldSidecar), "error", err)
							}
						} else if err == nil {
							log.Info(fmt.Sprintf("Removed obsolete sidecar %s", oldSidecar))
							stopDirs := getPruneStopDirs()
							if targetPath != "" {
								stopDirs = append(stopDirs, targetPath)
							}
							pruneEmptyParentDirs(filepath.Dir(oldSidecar), stopDirs)
						}
					}
				}
			}

			if removedLdso {
				cmd := execCommand("sudo", "ldconfig")
				_ = cmd.Run()
			}
			if removedUdev {
				cmd := execCommand("sudo", "udevadm", "control", "--reload-rules")
				_ = cmd.Run()
				cmdTrigger := execCommand("sudo", "udevadm", "trigger")
				_ = cmdTrigger.Run()
			}

			newBinarySet := make(map[string]bool, len(updatedBinaries)*2)
			for _, b := range updatedBinaries {
				newBinarySet[b] = true
				newBinarySet[filepath.Base(b)] = true
			}

			if targetPath != "" {
				for _, oldBin := range oldInstalledBinaries {
					if strings.TrimSpace(oldBin) == "" {
						continue
					}
					if !newBinarySet[oldBin] && !newBinarySet[filepath.Base(oldBin)] {
						binName := oldBin
						if filepath.Base(binName) != binName || filepath.IsAbs(binName) {
							binName = filepath.Base(oldBin)
						}
						binPath, err := safeDeletePath(targetPath, binName)
						if err != nil {
							log.Warn(fmt.Sprintf("Skipping unsafe binary name %q", oldBin), "error", err)
							continue
						}
						if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
							log.Warn(fmt.Sprintf("Failed to remove obsolete binary %s", binPath), "error", err)
						} else if err == nil {
							log.Info(fmt.Sprintf("Removed obsolete binary %s", binPath))
						}
					}
				}
			}

			log.Info(fmt.Sprintf("Successfully updated %s", app.Repository))
			state.LogHistory("update", app.Repository, latestTag)

			// Incremental checkpointing: atomically persist state per completed app update
			if !r.NoSaveState && !r.DryRun {
				if err := state.Mutate(func(currentSt *state.State) error {
					if currentSt.Apps == nil {
						currentSt.Apps = make(map[string]*state.InstalledApp)
					}
					if currentApp, ok := currentSt.Apps[app.Repository]; ok && currentApp != nil {
						currentApp.Version = latestTag
						currentApp.InstalledSidecars = updatedSidecars
						currentApp.InstalledBinaries = updatedBinaries
					} else {
						app.Version = latestTag
						app.InstalledSidecars = updatedSidecars
						app.InstalledBinaries = updatedBinaries
						currentSt.Apps[app.Repository] = app
					}
					return nil
				}); err != nil {
					log.Warn(fmt.Sprintf("Failed to checkpoint state for %s", app.Repository), "error", err)
				}
			}

			mu.Lock()
			successfulUpdates[app.Repository] = latestTag
			if appEntry, ok := st.Apps[app.Repository]; ok {
				appEntry.Version = latestTag
				appEntry.InstalledSidecars = updatedSidecars
				appEntry.InstalledBinaries = updatedBinaries
			}
			mu.Unlock()

			return nil
		})
	}

	waitErr := g.Wait()

	if len(successfulUpdates) > 0 && !r.NoSaveState && !r.DryRun {
		pterm.Success.Printf("Successfully updated %d application(s)\n", len(successfulUpdates))
	}

	return waitErr
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
