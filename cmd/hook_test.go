package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleHook_SavesPostInstallHook(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	scriptFile := filepath.Join(tmpDir, "post-install.sh")
	scriptContent := "#!/bin/bash\necho 'hello from post-install' > /tmp/hook.txt\n"
	require.NoError(t, os.WriteFile(scriptFile, []byte(scriptContent), 0755))

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:     "owner/repo",
			HookEvent:      "post-install",
			HookScriptPath: scriptFile,
		},
	}

	err := r.handleHook()
	require.NoError(t, err)

	st, err := state.LoadState()
	require.NoError(t, err)
	require.NotNil(t, st.Apps["owner/repo"])
	assert.Equal(t, scriptContent, st.Apps["owner/repo"].Hooks["post-install"])
}

func TestHandleHook_SavesPreUninstallHook(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	scriptFile := filepath.Join(tmpDir, "pre-uninstall.sh")
	scriptContent := "#!/bin/sh\necho 'cleaning up'\n"
	require.NoError(t, os.WriteFile(scriptFile, []byte(scriptContent), 0755))

	r := &RootCLI{
		Hook: params.Hook{
			Event:      "pre-uninstall",
			Repository: "owner/tool",
			ScriptPath: scriptFile,
		},
	}

	err := r.handleHook()
	require.NoError(t, err)

	st, err := state.LoadState()
	require.NoError(t, err)
	require.NotNil(t, st.Apps["owner/tool"])
	assert.Equal(t, scriptContent, st.Apps["owner/tool"].Hooks["pre-uninstall"])
}

func TestHandleHook_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	scriptFile := filepath.Join(tmpDir, "test.sh")
	require.NoError(t, os.WriteFile(scriptFile, []byte("echo hi"), 0755))

	// Invalid event
	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository:     "owner/repo",
			HookEvent:      "invalid-event",
			HookScriptPath: scriptFile,
		},
	}
	assert.Error(t, r.handleHook())

	// Missing repo
	r = &RootCLI{
		ExecContext: params.ExecContext{
			Repository:     "",
			HookEvent:      "post-install",
			HookScriptPath: scriptFile,
		},
	}
	assert.Error(t, r.handleHook())

	// Missing script path
	r = &RootCLI{
		ExecContext: params.ExecContext{
			Repository:     "owner/repo",
			HookEvent:      "post-install",
			HookScriptPath: "",
		},
	}
	assert.Error(t, r.handleHook())

	// Non-existent script path
	r = &RootCLI{
		ExecContext: params.ExecContext{
			Repository:     "owner/repo",
			HookEvent:      "post-install",
			HookScriptPath: filepath.Join(tmpDir, "does-not-exist.sh"),
		},
	}
	assert.Error(t, r.handleHook())
}

func TestRunPostInstallHook_ExecutesScript(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	markerFile := filepath.Join(tmpDir, "post_marker.txt")
	scriptContent := "#!/bin/sh\necho 'success' > " + markerFile + "\n"

	st, err := state.LoadState()
	require.NoError(t, err)
	st.Apps["owner/testapp"] = &state.InstalledApp{
		Repository: "owner/testapp",
		Hooks: map[string]string{
			"post-install": scriptContent,
		},
	}
	require.NoError(t, st.Save())

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/testapp",
		},
	}

	err = r.runPostInstallHook("owner/testapp")
	require.NoError(t, err)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "success")
}

func TestRunPostInstallHook_FailureReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	scriptContent := "#!/bin/sh\nexit 42\n"

	st, err := state.LoadState()
	require.NoError(t, err)
	st.Apps["owner/failapp"] = &state.InstalledApp{
		Repository: "owner/failapp",
		Hooks: map[string]string{
			"post-install": scriptContent,
		},
	}
	require.NoError(t, st.Save())

	r := &RootCLI{
		ExecContext: params.ExecContext{
			Repository: "owner/failapp",
		},
	}

	err = r.runPostInstallHook("owner/failapp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "post-install hook failed")
}

func TestRemoveApp_ExecutesPreUninstallHook(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	markerFile := filepath.Join(tmpDir, "pre_uninstall_marker.txt")
	scriptContent := "#!/bin/sh\necho 'pre-uninstall ran' > " + markerFile + "\n"

	binFile := filepath.Join(tmpDir, "testbin")
	require.NoError(t, os.WriteFile(binFile, []byte("binary data"), 0755))

	st, err := state.LoadState()
	require.NoError(t, err)
	st.Apps["owner/removable"] = &state.InstalledApp{
		Repository:        "owner/removable",
		TargetPath:        tmpDir,
		InstalledBinaries: []string{binFile},
		Hooks: map[string]string{
			"pre-uninstall": scriptContent,
		},
	}
	require.NoError(t, st.Save())

	err = RemoveApp("owner/removable", false, false)
	require.NoError(t, err)

	// Verify pre-uninstall hook ran
	markerData, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	assert.Contains(t, string(markerData), "pre-uninstall ran")

	// Verify app removed from state
	st2, err := state.LoadState()
	require.NoError(t, err)
	assert.Nil(t, st2.Apps["owner/removable"])
}

func TestRemoveApp_PreUninstallHookFailureAbortsRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	binFile := filepath.Join(tmpDir, "testbin")
	require.NoError(t, os.WriteFile(binFile, []byte("binary data"), 0755))

	st, err := state.LoadState()
	require.NoError(t, err)
	st.Apps["owner/failremove"] = &state.InstalledApp{
		Repository:        "owner/failremove",
		TargetPath:        tmpDir,
		InstalledBinaries: []string{binFile},
		Hooks: map[string]string{
			"pre-uninstall": "#!/bin/sh\nexit 1\n",
		},
	}
	require.NoError(t, st.Save())

	err = RemoveApp("owner/failremove", false, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pre-uninstall hook failed")

	// Verify binary was NOT removed
	_, err = os.Stat(binFile)
	assert.NoError(t, err)

	// Verify app is still in state
	st2, err := state.LoadState()
	require.NoError(t, err)
	assert.NotNil(t, st2.Apps["owner/failremove"])
}

func TestHook_ParamsRunnerIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	origXdgData := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdgData }()

	scriptFile := filepath.Join(tmpDir, "hook.sh")
	require.NoError(t, os.WriteFile(scriptFile, []byte("echo runner"), 0755))

	hook := &params.Hook{
		Event:      "post-install",
		Repository: "test/runner-app",
		ScriptPath: scriptFile,
	}

	err := hook.Run()
	require.NoError(t, err)

	st, err := state.LoadState()
	require.NoError(t, err)
	require.NotNil(t, st.Apps["test/runner-app"])
	assert.Equal(t, "echo runner", st.Apps["test/runner-app"].Hooks["post-install"])
}
