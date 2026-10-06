package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDisplayPreferences(t *testing.T) {
	origNoColor := os.Getenv("NO_COLOR")
	defer func() {
		_ = os.Setenv("NO_COLOR", origNoColor)
		pterm.EnableColor()
	}()

	_ = os.Unsetenv("NO_COLOR")
	ApplyDisplayPreferences(true, false)
	assert.Equal(t, "1", os.Getenv("NO_COLOR"))

	out := pterm.Success.Sprint("test")
	assert.NotContains(t, out, "\033[")
}

func TestResolveDisplayPreferences(t *testing.T) {
	t.Run("Flags NoEmojis and NoColor", func(t *testing.T) {
		r := &RootCLI{}
		flags := &params.CommonInstallFlags{
			NoColor:  true,
			NoEmojis: true,
		}

		resolveDisplayPreferences(flags, r)
		assert.True(t, r.NoColor)
		assert.True(t, r.DisableIcons)
		assert.True(t, flags.NoEmojis)
		assert.True(t, flags.NoColor)
	})

	t.Run("Env vars GH_PT_NO_COLOR and GH_PT_NO_EMOJIS", func(t *testing.T) {
		origColor := os.Getenv("GH_PT_NO_COLOR")
		origEmojis := os.Getenv("GH_PT_NO_EMOJIS")
		defer func() {
			_ = os.Setenv("GH_PT_NO_COLOR", origColor)
			_ = os.Setenv("GH_PT_NO_EMOJIS", origEmojis)
		}()

		_ = os.Setenv("GH_PT_NO_COLOR", "1")
		_ = os.Setenv("GH_PT_NO_EMOJIS", "1")

		r := &RootCLI{}
		flags := &params.CommonInstallFlags{}

		resolveDisplayPreferences(flags, r)
		assert.True(t, r.NoColor)
		assert.True(t, r.DisableIcons)
		assert.True(t, flags.NoColor)
		assert.True(t, flags.NoEmojis)
	})

	t.Run("Config yaml no_color and no_emojis", func(t *testing.T) {
		tmpDir := t.TempDir()
		origConfigHome := xdg.ConfigHome
		xdg.ConfigHome = tmpDir
		defer func() { xdg.ConfigHome = origConfigHome }()

		cfgPath := filepath.Join(tmpDir, "gh-pt", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0755))
		require.NoError(t, os.WriteFile(cfgPath, []byte("no_color: true\nno_emojis: true\n"), 0644))

		r := &RootCLI{}
		flags := &params.CommonInstallFlags{}

		resolveDisplayPreferences(flags, r)
		assert.True(t, r.NoColor)
		assert.True(t, r.DisableIcons)
		assert.True(t, flags.NoColor)
		assert.True(t, flags.NoEmojis)
	})
}

func TestShowInfo_NoEmojis_Flag(t *testing.T) {
	mock := &mockGhClient{
		releases: []Release{
			{
				ID:      1,
				TagName: "v1.0.0",
				Name:    "Release v1.0.0",
				Assets: []ReleaseAsset{
					{ID: 10, Name: "app-amd64.tar.gz", Size: 2048},
				},
			},
		},
	}

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:   "owner/repo",
			Show:         true,
			ShowVersions: 10,
			ShowAssets:   10,
			CommonInstallFlags: params.CommonInstallFlags{
				NoEmojis: true,
			},
		},
	}

	out := captureOutput(func() {
		err := showInfoWithClient(r, mock)
		assert.NoError(t, err)
	})

	assert.NotContains(t, out, "📦")
	assert.NotContains(t, out, "📂")
	assert.Contains(t, out, "--- VERSIONS ---")
	assert.Contains(t, out, "--- ASSETS ---")
}
