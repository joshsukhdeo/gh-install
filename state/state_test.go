package state

import (
	"os"
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

		_ = st.AddApp(app)
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

		_ = st.AddApp(app)
		assert.Len(t, st.Apps, 1) // Should still be 1

		loadedApp := st.Apps["junegunn/fzf"]
		assert.Equal(t, "v1.4.0", loadedApp.Version)
	})

	t.Run("AddApp_CloneAndForkState", func(t *testing.T) {
		st, err := LoadState()
		require.NoError(t, err)

		cloneApp := &InstalledApp{
			Repository: "sharkdp/fd",
			TargetPath: "/home/user/src/fd",
			Clone:      true,
		}
		forkApp := &InstalledApp{
			Repository: "BurntSushi/ripgrep",
			TargetPath: "/home/user/projects/ripgrep",
			Fork:       true,
		}

		_ = st.AddApp(cloneApp)
		_ = st.AddApp(forkApp)

		st2, err := LoadState()
		require.NoError(t, err)
		assert.True(t, st2.Apps["sharkdp/fd"].Clone)
		assert.False(t, st2.Apps["sharkdp/fd"].Fork)
		assert.True(t, st2.Apps["BurntSushi/ripgrep"].Fork)
		assert.False(t, st2.Apps["BurntSushi/ripgrep"].Clone)
	})

	t.Run("AddApp_CompileScript", func(t *testing.T) {
		st, err := LoadState()
		require.NoError(t, err)

		app := &InstalledApp{
			Repository:    "neovim/neovim",
			CompileScript: "/home/user/.config/gh-install/scripts/compile-neovim.sh",
		}

		_ = st.AddApp(app)

		st2, err := LoadState()
		require.NoError(t, err)
		assert.Equal(t, "/home/user/.config/gh-install/scripts/compile-neovim.sh", st2.Apps["neovim/neovim"].CompileScript)
	})

	t.Run("LoadState_FileReadErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "readerror"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "readerror", "gh-install")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		statePath := filepath.Join(stateDir, "state.json")
		// Create a directory instead of a file so os.ReadFile will fail
		err = os.MkdirAll(statePath, 0755)
		require.NoError(t, err)

		s, err := LoadState()
		assert.Error(t, err)
		assert.Nil(t, s)
	})

	t.Run("LoadState_InvalidJsonReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "invalid"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "invalid", "gh-install")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		statePath := filepath.Join(stateDir, "state.json")
		err = os.WriteFile(statePath, []byte("invalid json"), 0644)
		require.NoError(t, err)

		s, err := LoadState()
		assert.Error(t, err)
		assert.Nil(t, s)
	})

	t.Run("Save_MkdirErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "mkdirerror"))
		xdg.Reload()
		// Create a file where directory should be
		err := os.MkdirAll(filepath.Join(tmpDir, "mkdirerror"), 0755)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "mkdirerror", "gh-install"), []byte("file"), 0644)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("Save_FileWriteErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "writeerror"))
		xdg.Reload()
		// Create a directory where file should be
		err := os.MkdirAll(filepath.Join(tmpDir, "writeerror", "gh-install", "state.json"), 0755)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("Save_LockErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "lockerror"))
		xdg.Reload()
		err := os.MkdirAll(filepath.Join(tmpDir, "lockerror", "gh-install"), 0755)
		require.NoError(t, err)

		// Create a directory where the lock file would go, so lock.Lock() fails
		lockPath := filepath.Join(tmpDir, "lockerror", "gh-install", "state.json.lock")
		err = os.MkdirAll(lockPath, 0755)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("LoadState_AppsIsNil", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "nilapps"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "nilapps", "gh-install")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		statePath := filepath.Join(stateDir, "state.json")
		err = os.WriteFile(statePath, []byte(`{}`), 0644)
		require.NoError(t, err)

		s, err := LoadState()
		require.NoError(t, err)
		assert.NotNil(t, s.Apps)
	})
}
