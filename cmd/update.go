package cmd

import (
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joshsukhdeo/gh-install/params"
	"github.com/joshsukhdeo/gh-install/release"
	"github.com/joshsukhdeo/gh-install/state"
	"github.com/rs/zerolog/log"
)

func DoUpdate(r *RootCLI, ghClient *api.RESTClient) error {
	st, err := state.LoadState()
	if err != nil {
		return err
	}

	for _, app := range st.Apps {
		specificallyTargeted := (r.Repository != "" && strings.EqualFold(r.Repository, app.Repository))

		if r.Repository != "" && !specificallyTargeted {
			continue // specifically targeted another app
		}

		if app.Disabled && !specificallyTargeted {
			continue // skip disabled unless specifically targeted
		}

		if app.Pinned && !specificallyTargeted {
			log.Info().Msgf("Skipping %s (pinned at %s)", app.Repository, app.Version)
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

		log.Info().Msgf("Updating %s...", app.Repository)

		// Create fresh params for this app based on stored state
		appParams := *(*params.CLI)(r)
		appParams.Repository = app.Repository
		appParams.TargetPath = app.TargetPath
		appParams.Global = app.Global
		appParams.ReleaseAsset = app.ReleaseAsset
		appParams.ReleaseAssetRegexp = app.ReleaseRegexp
		appParams.Rename = app.Rename
		if len(app.Type) > 0 {
			appParams.Type = app.Type
		}
		appParams.All = app.All
		appParams.AssetBinaries = app.AssetBinaries
		appParams.AssetBinariesRegexp = app.AssetBinariesRegexp
		// Reset version to latest to ensure we get the latest
		appParams.ReleaseVersion = "latest"

		installRelease := release.MakeGithubRelease(&appParams, ghClient)
		err := installRelease.Install()
		if err != nil {
			log.Error().Err(err).Msgf("Failed to update %s", app.Repository)
		} else {
			log.Info().Msgf("Successfully updated %s", app.Repository)
		}
	}

	return nil
}
