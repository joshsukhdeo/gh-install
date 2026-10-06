package release

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteSymlinkInstall(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	targetPath := filepath.Join(homeDir, ".local", "bin")
	require.NoError(t, os.MkdirAll(targetPath, 0755))

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	xdg.Reload()

	assetFile := filepath.Join(tmpDir, "jq-linux-amd64")
	require.NoError(t, os.WriteFile(assetFile, []byte("dummy binary"), 0755))

	r := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository:         "jqlang/jq",
			CommonInstallFlags: params.CommonInstallFlags{TargetPath: targetPath},
		},
	}

	binaries := []*selector.SelectorItem{
		{
			Name:         "jq",
			DownloadPath: assetFile,
		},
	}

	symlinkDir, err := r.executeSymlinkInstall(binaries, assetFile)
	assert.NoError(t, err)

	// Canonical ADR-004 package path: $XDG_DATA_HOME/gh-pt/packages/jqlang/jq
	expectedAppDir := filepath.Join(xdg.DataHome, "gh-pt", "packages", "jqlang", "jq")
	assert.Equal(t, expectedAppDir, symlinkDir)

	// Verify the file was copied to the app dir
	copiedFile := filepath.Join(expectedAppDir, "jq-linux-amd64")
	assert.FileExists(t, copiedFile)

	// Verify symlink was created in target path
	symlinkTarget := filepath.Join(targetPath, "jq")
	assert.FileExists(t, symlinkTarget)

	// Verify symlink points to the copied file
	linkInfo, err := os.Readlink(symlinkTarget)
	assert.NoError(t, err)
	assert.Equal(t, copiedFile, linkInfo)

	// Verify fallback to ~/src/apps when legacy folder exists
	legacyAppDir := filepath.Join(homeDir, "src", "apps", "legacy", "app")
	require.NoError(t, os.MkdirAll(legacyAppDir, 0755))
	rLegacy := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository: "legacy/app",
		},
	}
	assert.Equal(t, legacyAppDir, rLegacy.resolvePackageDir("legacy", "app"))
}
