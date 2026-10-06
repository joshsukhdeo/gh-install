package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
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
