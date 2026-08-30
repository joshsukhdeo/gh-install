package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshsukhdeo/gh-install/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/adrg/xdg"
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

func TestRmState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, err := state.LoadState()
	require.NoError(t, err)

	binDir := filepath.Join(tmpDir, "bin")
	err = os.MkdirAll(binDir, 0755)
	require.NoError(t, err)

	testBin := filepath.Join(binDir, "my-repo")
	err = os.WriteFile(testBin, []byte("test"), 0755)
	require.NoError(t, err)

	app := &state.InstalledApp{
		Repository: "owner/my-repo",
		TargetPath: binDir,
		PackageNames: []string{"test-pkg"},
	}
	st.AddApp(app)

	// Temporarily override execCommand for tests if needed for uninstalling packages,
	// but the function uses exec.Command directly. It doesn't use seams!
	// So we'll hit errors during os.Remove because of it.

	err = RmState("my-repo")
	assert.NoError(t, err)

	st2, _ := state.LoadState()
	assert.Empty(t, st2.Apps)
}

func TestListState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()

	st, _ := state.LoadState()
	app := &state.InstalledApp{
		Repository: "owner/my-repo",
		Version: "1.0",
	}
	st.AddApp(app)

	err := ListState()
	assert.NoError(t, err)
}
