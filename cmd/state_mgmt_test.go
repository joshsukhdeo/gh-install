package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to mock exec.Command
func helperCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(0)
}

func setupState(t *testing.T) string {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "test/repo1",
		TargetPath: tmpDir,
		Rename:     map[string]string{"binary1": "bin1"},
		Pinned:     false,
	}))
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository:    "test/repo2",
		TargetPath:    tmpDir,
		CompileScript: filepath.Join(tmpDir, "script.sh"),
	}))

	return tmpDir
}

func TestRmStateOnly(t *testing.T) {
	setupState(t)
	err := RmStateOnly("repo1")
	require.NoError(t, err)

	st, _ := state.LoadState()
	_, exists := st.Apps["test/repo1"]
	assert.False(t, exists)
}

func TestRemoveApp(t *testing.T) {
	tmpDir := setupState(t)

	binPath := filepath.Join(tmpDir, "bin1")
	require.NoError(t, os.WriteFile(binPath, []byte("data"), 0755))

	origExecCommand := execCommand
	execCommand = helperCommand
	defer func() { execCommand = origExecCommand }()

	err := RemoveApp("repo1", false)
	require.NoError(t, err)

	st, _ := state.LoadState()
	_, exists := st.Apps["test/repo1"]
	assert.False(t, exists)
	assert.NoFileExists(t, binPath)
}

func TestPurgeApp(t *testing.T) {
	tmpDir := setupState(t)

	scriptPath := filepath.Join(tmpDir, "script.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("data"), 0755))

	err := RemoveApp("repo2", true)
	require.NoError(t, err)

	st, _ := state.LoadState()
	_, exists := st.Apps["test/repo2"]
	assert.False(t, exists)
	assert.NoFileExists(t, scriptPath)
}

func TestPinAppState(t *testing.T) {
	setupState(t)

	err := PinAppState("repo1")
	require.NoError(t, err)

	st, _ := state.LoadState()
	assert.True(t, st.Apps["test/repo1"].Pinned)
}

func TestListState(t *testing.T) {
	setupState(t)
	err := ListState()
	assert.NoError(t, err)
}

func TestRemoveApp_CleansUpSidecarsAndSymlinkDir(t *testing.T) {
	tmpDir := setupState(t)

	// Create package directory (SymlinkDir) with contents
	symlinkDir := filepath.Join(tmpDir, "packages", "test-repo3")
	require.NoError(t, os.MkdirAll(symlinkDir, 0755))
	// Add gh-pt managed marker for safe removal
	require.NoError(t, safety.WriteGhptManagedMarker(symlinkDir))
	targetBinary := filepath.Join(symlinkDir, "binary3")
	require.NoError(t, os.WriteFile(targetBinary, []byte("pkg binary data"), 0755))
	extraFile := filepath.Join(symlinkDir, "extra.txt")
	require.NoError(t, os.WriteFile(extraFile, []byte("extra data"), 0644))

	// Create binary symlink in TargetPath pointing to symlinkDir binary
	binSymlink := filepath.Join(tmpDir, "repo3")
	require.NoError(t, os.Symlink(targetBinary, binSymlink))

	// Create sidecar files: regular file, symlink, and dangling symlink
	sidecarFile := filepath.Join(tmpDir, "sidecar.json")
	require.NoError(t, os.WriteFile(sidecarFile, []byte(`{"plugin":true}`), 0644))

	sidecarSymlink := filepath.Join(tmpDir, "sidecar-link.txt")
	require.NoError(t, os.Symlink(extraFile, sidecarSymlink))

	danglingSidecarSymlink := filepath.Join(tmpDir, "dangling-sidecar.so")
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "nonexistent-target"), danglingSidecarSymlink))

	// Create canonical sidecar directory $XDG_DATA_HOME/gh-pt/sidecars/{repo}
	canonicalSidecarDir := filepath.Join(xdg.DataHome, "gh-pt", "sidecars", "test/repo3")
	require.NoError(t, os.MkdirAll(canonicalSidecarDir, 0755))
	// Add gh-pt managed marker for safe removal
	require.NoError(t, safety.WriteGhptManagedMarker(canonicalSidecarDir))
	canonicalFile := filepath.Join(canonicalSidecarDir, "canonical.cfg")
	require.NoError(t, os.WriteFile(canonicalFile, []byte("cfg"), 0644))

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository:        "test/repo3",
		TargetPath:        tmpDir,
		SymlinkDir:        symlinkDir,
		InstalledSidecars: []string{sidecarFile, sidecarSymlink, danglingSidecarSymlink},
	}))

	origExecCommand := execCommand
	execCommand = helperCommand
	defer func() { execCommand = origExecCommand }()

	err = RemoveApp("test/repo3", true)
	require.NoError(t, err)

	// Assert that all sidecars are gone from disk
	assert.NoFileExists(t, sidecarFile)
	_, err = os.Lstat(sidecarSymlink)
	assert.True(t, os.IsNotExist(err), "sidecar symlink should not exist on disk")
	_, err = os.Lstat(danglingSidecarSymlink)
	assert.True(t, os.IsNotExist(err), "dangling sidecar symlink should not exist on disk")

	// Assert binary in target path is removed
	_, err = os.Lstat(binSymlink)
	assert.True(t, os.IsNotExist(err), "binary symlink in target path should not exist on disk")

	// Assert SymlinkDir and canonical sidecar directory are gone from disk
	assert.NoDirExists(t, symlinkDir)
	assert.NoDirExists(t, canonicalSidecarDir)

	// Assert app is removed from state
	st, _ = state.LoadState()
	_, exists := st.Apps["test/repo3"]
	assert.False(t, exists)
}

func TestRemoveApp_PrunesEmptyParentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	origDataHome := os.Getenv("XDG_DATA_HOME")
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	defer func() {
		_ = os.Setenv("XDG_DATA_HOME", origDataHome)
		xdg.Reload()
	}()

	// Create deeply nested sidecar inside a data directory: <tmpDir>/data/myapp/nested/asset.json
	dataDir := filepath.Join(tmpDir, "data")
	nestedDir := filepath.Join(dataDir, "myapp", "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))
	sidecarFile := filepath.Join(nestedDir, "asset.json")
	require.NoError(t, os.WriteFile(sidecarFile, []byte("{}"), 0644))

	// Add a sibling file in dataDir so dataDir is non-empty and preserved
	siblingFile := filepath.Join(dataDir, "sibling.txt")
	require.NoError(t, os.WriteFile(siblingFile, []byte("keep"), 0644))

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository:        "owner/myapp",
		TargetPath:        tmpDir,
		InstalledSidecars: []string{sidecarFile},
	}))

	err = RemoveApp("owner/myapp", false)
	require.NoError(t, err)

	// Sidecar file must be deleted
	assert.NoFileExists(t, sidecarFile)
	// Empty parent directories nested/ and myapp/ must be pruned
	assert.NoDirExists(t, nestedDir)
	assert.NoDirExists(t, filepath.Join(dataDir, "myapp"))
	// dataDir has sibling.txt so it must remain intact
	assert.DirExists(t, dataDir)
	assert.FileExists(t, siblingFile)
	// tmpDir (XDG_DATA_HOME stop-dir) must remain intact
	assert.DirExists(t, tmpDir)
}

func TestIsSystemOrProtectedDir(t *testing.T) {
	assert.True(t, isSystemOrProtectedDir(""))
	assert.True(t, isSystemOrProtectedDir("/"))
	assert.True(t, isSystemOrProtectedDir("."))
	assert.True(t, isSystemOrProtectedDir("/usr"))
	assert.True(t, isSystemOrProtectedDir("/usr/bin"))
	assert.True(t, isSystemOrProtectedDir("/usr/local"))
	assert.True(t, isSystemOrProtectedDir("/usr/local/bin"))
	assert.True(t, isSystemOrProtectedDir("/etc"))
	assert.True(t, isSystemOrProtectedDir("/var"))
	assert.True(t, isSystemOrProtectedDir("/opt"))

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		assert.True(t, isSystemOrProtectedDir(home))
		assert.True(t, isSystemOrProtectedDir(filepath.Join(home, ".local")))
		assert.True(t, isSystemOrProtectedDir(filepath.Join(home, ".local", "bin")))
		assert.True(t, isSystemOrProtectedDir(filepath.Join(home, "bin")))
	}

	// Safe application package directory should NOT be protected
	tmpDir := t.TempDir()
	appPkgDir := filepath.Join(tmpDir, "gh-pt", "packages", "owner", "repo")
	assert.False(t, isSystemOrProtectedDir(appPkgDir))
}

func TestRemoveApp_ProtectsSystemAndTargetDirs(t *testing.T) {
	tmpDir := t.TempDir()
	origXdg := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	defer func() {
		os.Setenv("XDG_DATA_HOME", origXdg)
		xdg.Reload()
	}()

	// Simulate dangerous state where SymlinkDir points to TargetPath
	targetDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(targetDir, 0755))
	keepFile := filepath.Join(targetDir, "important_binary")
	require.NoError(t, os.WriteFile(keepFile, []byte("preserve me"), 0755))

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "danger/app",
		TargetPath: targetDir,
		SymlinkDir: targetDir, // Accidentally identical to TargetPath
	}))

	err = RemoveApp("danger/app", true)
	require.NoError(t, err)

	// TargetDir and files inside it MUST NOT be wiped
	assert.DirExists(t, targetDir)
	assert.FileExists(t, keepFile)
}

func TestRemoveApp_CleansUpDriverManifests(t *testing.T) {
	tmpDir := t.TempDir()
	origXdg := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	defer func() {
		os.Setenv("XDG_DATA_HOME", origXdg)
		xdg.Reload()
	}()

	manifestDir := filepath.Join(tmpDir, "vulkan", "icd.d")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))
	manifestFile := filepath.Join(manifestDir, "gh-pt-intel-compute-runtime.json")
	require.NoError(t, os.WriteFile(manifestFile, []byte(`{"ICD":{}}`), 0644))

	pkgDir := filepath.Join(tmpDir, "packages", "intel", "compute-runtime")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, safety.WriteGhptManagedMarker(pkgDir))

	st, err := state.LoadState()
	require.NoError(t, err)

	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository:        "intel/compute-runtime",
		TargetPath:        filepath.Join(tmpDir, "bin"),
		SymlinkDir:        pkgDir,
		Driver:            "vulkan",
		InstalledSidecars: []string{manifestFile},
	}))

	err = RemoveApp("intel/compute-runtime", true)
	require.NoError(t, err)

	// Manifest and package dir should be cleanly unlinked
	assert.NoFileExists(t, manifestFile)
	assert.NoDirExists(t, pkgDir)
}
