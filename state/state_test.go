package state

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateManagement(t *testing.T) {
	// Setup hermetic test environment
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	t.Run("LoadState_CreatesEmptyState", func(t *testing.T) {
		st, err := LoadState()
		require.NoError(t, err)
		assert.NotNil(t, st)
		assert.Empty(t, st.Apps)
	})

	t.Run("AddApp_And_Save", func(t *testing.T) {
		st, err := LoadState()
		require.NoError(t, err)

		app := &InstalledApp{
			Repository:    "junegunn/fzf",
			TargetPath:    "/tmp/bin",
			Global:        false,
			ReleaseAsset:  "fzf-linux",
			ReleaseRegexp: ".*",
			Version:       "v1.2.3",
			Rename:        map[string]string{"fzf-linux": "fzf"},
			Disabled:      false,
		}

		st.AddApp(app)
		assert.Len(t, st.Apps, 1)

		err = st.Save()
		require.NoError(t, err)

		// Verify it was written to disk
		statePath := filepath.Join(tmpDir, "gh-install", "state.json")
		assert.FileExists(t, statePath)

		// Load again to verify unmarshalling
		st2, err := LoadState()
		require.NoError(t, err)
		assert.Len(t, st2.Apps, 1)

		loadedApp, exists := st2.Apps["junegunn/fzf"]
		assert.True(t, exists)
		assert.Equal(t, "v1.2.3", loadedApp.Version)
		assert.Equal(t, "fzf", loadedApp.Rename["fzf-linux"])
	})

	t.Run("AddApp_OverwritesExisting", func(t *testing.T) {
		st, err := LoadState()
		require.NoError(t, err)

		app := &InstalledApp{
			Repository: "junegunn/fzf",
			Version:    "v1.4.0", // Updated version
		}

		st.AddApp(app)
		assert.Len(t, st.Apps, 1) // Should still be 1

		loadedApp := st.Apps["junegunn/fzf"]
		assert.Equal(t, "v1.4.0", loadedApp.Version)
	})
}
