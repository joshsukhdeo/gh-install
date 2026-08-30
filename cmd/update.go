package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
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

func SetupTopgrade() error {
	topgradePath := filepath.Join(xdg.ConfigHome, "topgrade.toml")

	content, err := os.ReadFile(topgradePath)
	if err != nil {
		if os.IsNotExist(err) {
			content = []byte("[custom_commands]\n")
		} else {
			return err
		}
	}

	conf := string(content)
	if strings.Contains(conf, "\"gh-install\" =") {
		log.Info().Msg("Topgrade configuration already contains gh-install step.")
		return nil
	}

	if !strings.Contains(conf, "[custom_commands]") {
		conf += "\n[custom_commands]\n"
	}

	newLine := "\"gh-install\" = \"gh install -U\"\n"

	// Just append to the bottom or under [custom_commands]
	// A simple approach is appending. But if [custom_commands] is not at the bottom,
	// it might be cleaner to replace it.
	conf = strings.Replace(conf, "[custom_commands]", "[custom_commands]\n"+newLine, 1)

	err = os.WriteFile(topgradePath, []byte(conf), 0644)
	if err != nil {
		return err
	}

	log.Info().Msg("Successfully added gh-install to Topgrade custom commands.")
	return nil
}
