package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
		statePath := filepath.Join(tmpDir, "gh-pt", "state.json")
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
			CompileScript: "/home/user/.config/gh-pt/scripts/compile-neovim.sh",
		}

		_ = st.AddApp(app)

		st2, err := LoadState()
		require.NoError(t, err)
		assert.Equal(t, "/home/user/.config/gh-pt/scripts/compile-neovim.sh", st2.Apps["neovim/neovim"].CompileScript)
	})

	t.Run("LoadState_FileReadErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "readerror"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "readerror", "gh-pt")
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
		stateDir := filepath.Join(tmpDir, "invalid", "gh-pt")
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
		err = os.WriteFile(filepath.Join(tmpDir, "mkdirerror", "gh-pt"), []byte("file"), 0644)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("Save_FileWriteErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "writeerror"))
		xdg.Reload()
		// Create a directory where file should be
		err := os.MkdirAll(filepath.Join(tmpDir, "writeerror", "gh-pt", "state.json"), 0755)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("Save_LockErrorReturnsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "lockerror"))
		xdg.Reload()
		err := os.MkdirAll(filepath.Join(tmpDir, "lockerror", "gh-pt"), 0755)
		require.NoError(t, err)

		// Create a directory where the lock file would go, so lock.Lock() fails
		lockPath := filepath.Join(tmpDir, "lockerror", "gh-pt", "state.json.lock")
		err = os.MkdirAll(lockPath, 0755)
		require.NoError(t, err)

		st := &State{Apps: make(map[string]*InstalledApp)}
		err = st.Save()
		assert.Error(t, err)
	})

	t.Run("LoadState_AppsIsNil", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "nilapps"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "nilapps", "gh-pt")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		statePath := filepath.Join(stateDir, "state.json")
		err = os.WriteFile(statePath, []byte(`{}`), 0644)
		require.NoError(t, err)

		s, err := LoadState()
		require.NoError(t, err)
		assert.NotNil(t, s.Apps)
	})

	t.Run("MigrateV1toV2_Segregation", func(t *testing.T) {
		st := &State{
			Apps: map[string]*InstalledApp{
				"junegunn/fzf": {
					Repository:   "junegunn/fzf",
					Version:      "v1.2.3",
					TargetPath:   "/tmp/bin",
					PackageNames: []string{"fzf-bin"},
				},
				"sharkdp/fd": {
					Repository: "sharkdp/fd",
					TargetPath: "/home/user/src/fd",
					Clone:      true,
				},
				"BurntSushi/ripgrep": {
					Repository: "BurntSushi/ripgrep",
					TargetPath: "/home/user/projects/ripgrep",
					Fork:       true,
				},
				"my/tool": {
					Repository:   "my/tool",
					PackageNames: []string{"libfoo", "libbar"},
				},
			},
		}

		err := st.migrateV1toV2()
		require.NoError(t, err)
		assert.Equal(t, 2, st.Version)

		// Repos should be segregated from Apps
		assert.Len(t, st.Apps, 2)
		assert.Contains(t, st.Apps, "junegunn/fzf")
		assert.Contains(t, st.Apps, "my/tool")
		assert.NotContains(t, st.Apps, "sharkdp/fd")
		assert.NotContains(t, st.Apps, "BurntSushi/ripgrep")

		assert.Len(t, st.Repos, 2)
		assert.Contains(t, st.Repos, "sharkdp/fd")
		assert.Contains(t, st.Repos, "BurntSushi/ripgrep")
		assert.True(t, st.Repos["sharkdp/fd"].Clone)
		assert.True(t, st.Repos["BurntSushi/ripgrep"].Fork)

		// SystemPackages should be aggregated and migrated
		assert.Contains(t, st.SystemPackages, "fzf-bin")
		assert.Contains(t, st.SystemPackages, "libfoo")
		assert.Contains(t, st.SystemPackages, "libbar")
		assert.Equal(t, []string{"fzf-bin"}, st.Apps["junegunn/fzf"].SystemPackages)
		assert.Equal(t, []string{"libfoo", "libbar"}, st.Apps["my/tool"].SystemPackages)

		// Hooks should be initialized
		assert.NotNil(t, st.Hooks)
	})

	t.Run("MigrateV1toV2_Idempotent", func(t *testing.T) {
		st := &State{
			Version: 2,
			Apps: map[string]*InstalledApp{
				"app1": {Repository: "app1"},
			},
			Repos: map[string]*InstalledApp{
				"repo1": {Repository: "repo1", Clone: true},
			},
		}

		err := st.migrateV1toV2()
		require.NoError(t, err)
		assert.Equal(t, 2, st.Version)
		assert.Len(t, st.Apps, 1)
		assert.Len(t, st.Repos, 1)

		err = migrateV1toV2(st)
		require.NoError(t, err)
		assert.Equal(t, 2, st.Version)
	})

	t.Run("MigrateV1toV2_LoadStateMigration", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "migrateload"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "migrateload", "gh-pt")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		v1JSON := `{
  "apps": {
    "sharkdp/fd": {
      "repository": "sharkdp/fd",
      "target_path": "/home/user/src/fd",
      "clone": true
    },
    "junegunn/fzf": {
      "repository": "junegunn/fzf",
      "version": "v1.2.3",
      "target_path": "/tmp/bin",
      "package_names": ["fzf-pkg"]
    }
  }
}`
		statePath := filepath.Join(stateDir, "state.json")
		err = os.WriteFile(statePath, []byte(v1JSON), 0644)
		require.NoError(t, err)

		st, err := LoadState()
		require.NoError(t, err)
		assert.Equal(t, 3, st.Version)
		assert.NotContains(t, st.Apps, "sharkdp/fd")
		assert.Contains(t, st.Apps, "junegunn/fzf")
		assert.Contains(t, st.Repos, "sharkdp/fd")
		assert.Contains(t, st.SystemPackages, "fzf-pkg")
		assert.Equal(t, "/tmp", st.TargetBaseDir)
	})

	t.Run("MigrateV2toV3_TargetBaseDirInferred", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "migratev3"))
		xdg.Reload()
		stateDir := filepath.Join(tmpDir, "migratev3", "gh-pt")
		err := os.MkdirAll(stateDir, 0755)
		require.NoError(t, err)

		v2JSON := `{
  "version": 2,
  "apps": {
    "owner/app": {
      "repository": "owner/app",
      "target_path": "/home/user/.local/bin",
      "global": false
    }
  }
}`
		statePath := filepath.Join(stateDir, "state.json")
		err = os.WriteFile(statePath, []byte(v2JSON), 0644)
		require.NoError(t, err)

		st, err := LoadState()
		require.NoError(t, err)
		assert.Equal(t, 3, st.Version)
		assert.Equal(t, "/home/user/.local", st.TargetBaseDir)
		assert.False(t, st.Global)
		assert.NotNil(t, st.SourceRepos)
	})

	t.Run("InstalledApp_V2Fields", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "v2fields"))
		xdg.Reload()

		st, err := LoadState()
		require.NoError(t, err)

		app := &InstalledApp{
			Repository:        "owner/sidecar-app",
			Version:           "1.0.0",
			Hooks:             map[string]string{"post-install": "echo hello"},
			SystemPackages:    []string{"libssl3"},
			Sidecars:          `plugins/.*|assets/.*`,
			InstalledSidecars: []string{"/tmp/sidecars/owner/sidecar-app/plugin.so"},
		}

		err = st.AddApp(app)
		require.NoError(t, err)

		st2, err := LoadState()
		require.NoError(t, err)

		loaded, exists := st2.Apps["owner/sidecar-app"]
		require.True(t, exists)
		assert.Equal(t, map[string]string{"post-install": "echo hello"}, loaded.Hooks)
		assert.Equal(t, []string{"libssl3"}, loaded.SystemPackages)
		assert.Equal(t, `plugins/.*|assets/.*`, loaded.Sidecars)
		// SidecarTargetPath removed
		assert.Equal(t, []string{"/tmp/sidecars/owner/sidecar-app/plugin.so"}, loaded.InstalledSidecars)
	})

	t.Run("InstallMap_SerializationAndRetrieval", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "installmap"))
		xdg.Reload()

		st, err := LoadState()
		require.NoError(t, err)

		entry := &InstallMapEntry{
			Deps: &InstallStep{
				Pkg: &InstallPkg{
					Manager:   "apt",
					PackageID: "libssl-dev, cmake",
				},
				Files: []string{
					"/home/user/.local/.ghpt/share/dep1",
					"/home/user/.local/share/dep1",
				},
			},
			Installed: &InstallStep{
				Pkg: &InstallPkg{
					Manager:   "dpkg",
					PackageID: "my-app",
				},
				Files: []string{
					"/home/user/.local/.ghpt/bin/my-app",
					"/home/user/.local/bin/my-app",
				},
			},
		}

		err = st.SetInstallMap("owner/repo", entry)
		require.NoError(t, err)

		// Check raw json file to verify "install-map" key and subkeys
		rawBytes, err := os.ReadFile(GetStatePath())
		require.NoError(t, err)
		assert.Contains(t, string(rawBytes), `"install-map"`)
		assert.Contains(t, string(rawBytes), `"owner/repo"`)
		assert.Contains(t, string(rawBytes), `"deps"`)
		assert.Contains(t, string(rawBytes), `"installed"`)
		assert.Contains(t, string(rawBytes), `"manager"`)
		assert.Contains(t, string(rawBytes), `"PackageId"`)
		assert.Contains(t, string(rawBytes), `"files"`)

		// Load state and verify GetInstallMap
		st2, err := LoadState()
		require.NoError(t, err)

		retrieved := st2.GetInstallMap("owner/repo")
		require.NotNil(t, retrieved)
		require.NotNil(t, retrieved.Deps)
		assert.Equal(t, "apt", retrieved.Deps.Pkg.Manager)
		assert.Equal(t, "libssl-dev, cmake", retrieved.Deps.Pkg.PackageID)
		assert.Len(t, retrieved.Deps.Files, 2)

		require.NotNil(t, retrieved.Installed)
		assert.Equal(t, "dpkg", retrieved.Installed.Pkg.Manager)
		assert.Equal(t, "my-app", retrieved.Installed.Pkg.PackageID)
		assert.Len(t, retrieved.Installed.Files, 2)
	})

	t.Run("InstalledApp_AllowsUnsigned_Policy", func(t *testing.T) {
		// App that was previously signed
		signedApp := &InstalledApp{
			Repository: "secure/tool",
			WasSigned:  true,
		}
		// In bulk upgrade with global allow unsigned, previously signed app strictly rejects unsigned
		assert.False(t, signedApp.AllowsUnsigned(true, false))
		assert.False(t, signedApp.AllowsUnsigned(false, false))
		// If targeted explicitly by user, user override is respected
		assert.True(t, signedApp.AllowsUnsigned(true, true))

		// --insecure-allow-unsigned --force in generic execution context (targeted == false)
		// DOES NOT bypass previously signed packages (generic user, global, or all apps)
		assert.False(t, signedApp.AllowsUnsigned(true, false, true))

		// --insecure-allow-unsigned --force with specific named entry (targeted == true)
		// ALLOWS bypassing standard verification track (falling back to commit lineage)
		assert.True(t, signedApp.AllowsUnsigned(true, true, true))

		// App that never had signatures
		unsignedApp := &InstalledApp{
			Repository: "unsigned/tool",
			WasSigned:  false,
		}
		// In bulk upgrade, allowed if global flag is true
		assert.True(t, unsignedApp.AllowsUnsigned(true, false))
		assert.False(t, unsignedApp.AllowsUnsigned(false, false))

		// App with explicit item-level insecure permission saved in state
		itemPermittedApp := &InstalledApp{
			Repository:            "explicit/tool",
			InsecureAllowUnsigned: true,
			WasSigned:             false,
		}
		assert.True(t, itemPermittedApp.AllowsUnsigned(false, false))
		assert.True(t, itemPermittedApp.AllowsUnsigned(true, false))
	})

	t.Run("InstalledApp_IncludeSidecars_UnmarshalMigration", func(t *testing.T) {
		// 1. Legacy string mode "xdg_data_home"
		jsonLegacy := `{"repository":"legacy/app","include_sidecars":"xdg_data_home"}`
		var app1 InstalledApp
		err := json.Unmarshal([]byte(jsonLegacy), &app1)
		require.NoError(t, err)
		assert.True(t, app1.IncludeSidecars)
		assert.Equal(t, "xdg_data_home", app1.SidecarMode)

		// 2. Legacy string mode "same_dest"
		jsonCustom := `{"repository":"custom/app","include_sidecars":"same_dest"}`
		var app2 InstalledApp
		err = json.Unmarshal([]byte(jsonCustom), &app2)
		require.NoError(t, err)
		assert.True(t, app2.IncludeSidecars)
		assert.Equal(t, "same_dest", app2.SidecarMode)

		// 3. Modern boolean true
		jsonModern := `{"repository":"modern/app","include_sidecars":true,"sidecar_mode":"bin"}`
		var app3 InstalledApp
		err = json.Unmarshal([]byte(jsonModern), &app3)
		require.NoError(t, err)
		assert.True(t, app3.IncludeSidecars)
		assert.Equal(t, "bin", app3.SidecarMode)

		// 4. Boolean false / omitted
		jsonFalse := `{"repository":"off/app","include_sidecars":false}`
		var app4 InstalledApp
		err = json.Unmarshal([]byte(jsonFalse), &app4)
		require.NoError(t, err)
		assert.False(t, app4.IncludeSidecars)
	})

	t.Run("Mutate_ConcurrentNoLostUpdates", func(t *testing.T) {
		const numWorkers = 20
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			workerID := i
			go func() {
				defer wg.Done()
				repoName := fmt.Sprintf("owner/app-%d", workerID)
				err := Mutate(func(st *State) error {
					if st.Apps == nil {
						st.Apps = make(map[string]*InstalledApp)
					}
					st.Apps[repoName] = &InstalledApp{
						Repository: repoName,
						Version:    fmt.Sprintf("v1.%d.0", workerID),
					}
					return nil
				})
				assert.NoError(t, err)
			}()
		}

		wg.Wait()

		st, err := LoadState()
		require.NoError(t, err)
		for i := 0; i < numWorkers; i++ {
			repoName := fmt.Sprintf("owner/app-%d", i)
			app, exists := st.Apps[repoName]
			assert.True(t, exists, "app %s should exist in state without being lost to race condition", repoName)
			if exists {
				assert.Equal(t, fmt.Sprintf("v1.%d.0", i), app.Version)
			}
		}
	})
}
