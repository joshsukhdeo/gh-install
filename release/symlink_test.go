package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/joshsukhdeo/gh-pt/config"
	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/joshsukhdeo/gh-pt/state"
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
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
}

func TestResolvePackageDir(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	require.NoError(t, os.MkdirAll(homeDir, 0755))

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	xdg.Reload()

	r := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository: "legacy/app",
		},
	}

	canonicalDir := filepath.Join(xdg.DataHome, "gh-pt", "packages", "legacy", "app")

	// P1: 1. Do NOT hijack arbitrary ~/src/apps directory if neither configured nor in state
	arbitraryLegacyDir := filepath.Join(homeDir, "src", "apps", "legacy", "app")
	require.NoError(t, os.MkdirAll(arbitraryLegacyDir, 0755))

	resolved := r.resolvePackageDir("legacy", "app")
	assert.Equal(t, canonicalDir, resolved, "must not hijack arbitrary legacy directory on disk")

	// P1: 2. Return explicitly configured PackagePath
	cfgPkg := &config.Config{
		Paths: config.PathsConfig{
			PackagePath: filepath.Join(tmpDir, "custom-packages"),
		},
	}
	require.NoError(t, config.SaveConfig(cfgPkg))
	assert.Equal(t, filepath.Join(tmpDir, "custom-packages", "legacy", "app"), r.resolvePackageDir("legacy", "app"))

	// P1: 3. Return explicitly configured ClonePath
	cfgClone := &config.Config{
		Paths: config.PathsConfig{
			ClonePath: filepath.Join(tmpDir, "custom-clones"),
		},
	}
	require.NoError(t, config.SaveConfig(cfgClone))
	assert.Equal(t, filepath.Join(tmpDir, "custom-clones", "legacy", "app"), r.resolvePackageDir("legacy", "app"))

	// Clear config back to empty
	require.NoError(t, config.SaveConfig(&config.Config{}))

	// P1: 4. Check state (state.LoadState()): if app with repository was actually previously installed there
	st, err := state.LoadState()
	require.NoError(t, err)
	require.NoError(t, st.AddApp(&state.InstalledApp{
		Repository: "legacy/app",
		SymlinkDir: arbitraryLegacyDir,
	}))
	require.NoError(t, st.Save())

	assert.Equal(t, arbitraryLegacyDir, r.resolvePackageDir("legacy", "app"), "must return legacy dir when state records app was installed there")
}

func TestExecuteSymlinkInstall_FallbackOnSymlinkFailure(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	targetPath := filepath.Join(homeDir, ".local", "bin")
	require.NoError(t, os.MkdirAll(targetPath, 0755))

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	xdg.Reload()

	// Mock symlink creation to fail (simulating Windows without symlink privileges or unsupported FS)
	symlinkFunc = func(src, dst string) error {
		return errors.New("operation not supported: symlink privilege not held")
	}
	t.Cleanup(func() {
		symlinkFunc = os.Symlink
	})

	assetFile := filepath.Join(tmpDir, "mytool")
	require.NoError(t, os.WriteFile(assetFile, []byte("executable binary payload"), 0755))

	r := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository:         "myorg/mytool",
			CommonInstallFlags: params.CommonInstallFlags{TargetPath: targetPath},
		},
	}

	binaries := []*selector.SelectorItem{
		{
			Name:         "mytool",
			DownloadPath: assetFile,
		},
	}

	symlinkDir, err := r.executeSymlinkInstall(binaries, assetFile)
	assert.NoError(t, err, "installation should succeed via fallback when symlink fails")
	assert.NotEmpty(t, symlinkDir)

	installedTarget := filepath.Join(targetPath, "mytool")
	assert.FileExists(t, installedTarget)

	// Ensure the file content matches payload
	content, err := os.ReadFile(installedTarget)
	assert.NoError(t, err)
	assert.Equal(t, "executable binary payload", string(content))
}

func TestGetDefaultSidecarRegex(t *testing.T) {
	const (
		expectedBinPattern  = `\.so.*|\.dll|\.dylib|\.exe$`
		expectedDataPattern = `\.so.*|\.h$|\.hpp$|\.c$|\.cpp$|\.txt$|README.*|\.md$|\.json$|\.yaml$|\.yml$|\.toml$|\.conf$`
		expectedCustPattern = `\.so.*|\.h$|\.dll|\.dylib|\.txt$|README.*`
	)

	// 1. Explicit sidecar modes
	rBin := &GithubRelease{CliParams: &params.ExecContext{SidecarMode: "bin"}}
	assert.Equal(t, expectedBinPattern, rBin.getDefaultSidecarRegex("/any/path"))

	rXdg := &GithubRelease{CliParams: &params.ExecContext{SidecarMode: "xdg_data_home"}}
	assert.Equal(t, expectedDataPattern, rXdg.getDefaultSidecarRegex("/any/path"))

	rCust := &GithubRelease{CliParams: &params.ExecContext{SidecarMode: "custom-path:/opt/myfolder"}}
	assert.Equal(t, expectedCustPattern, rCust.getDefaultSidecarRegex("/opt/myfolder"))

	// 2. Destination path classification (without fragile substring matching)
	gr := &GithubRelease{}

	// Bin destinations
	assert.Equal(t, expectedBinPattern, gr.getDefaultSidecarRegex("/usr/local/bin"))
	assert.Equal(t, expectedBinPattern, gr.getDefaultSidecarRegex("/usr/bin"))
	assert.Equal(t, expectedBinPattern, gr.getDefaultSidecarRegex("/home/user/.local/bin"))

	// Bin destination with "xdg" in parent directory name should NOT be misclassified as xdg data
	assert.Equal(t, expectedBinPattern, gr.getDefaultSidecarRegex("/opt/xdg-runner/bin"))

	// XDG / data destinations
	assert.Equal(t, expectedDataPattern, gr.getDefaultSidecarRegex("/home/user/.local/share"))
	assert.Equal(t, expectedDataPattern, gr.getDefaultSidecarRegex("/usr/share"))
	assert.Equal(t, expectedDataPattern, gr.getDefaultSidecarRegex(filepath.Join(xdg.DataHome, "gh-pt", "sidecars")))

	// Custom destinations
	assert.Equal(t, expectedCustPattern, gr.getDefaultSidecarRegex("/opt/custom"))
	// Path with ".local/bin" substring but not a bin directory
	assert.Equal(t, expectedCustPattern, gr.getDefaultSidecarRegex("/home/user/.local/bin_backups/custom"))
}

func TestHasLocalMapLayout(t *testing.T) {
	tmpDir := t.TempDir()
	r := &GithubRelease{}

	// Empty dir: no local map
	assert.False(t, r.hasLocalMapLayout(tmpDir))

	// Dir with only LICENSE: no local map
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "LICENSE"), []byte("MIT"), 0644))
	assert.False(t, r.hasLocalMapLayout(tmpDir))

	// Standard share dir: has local map
	shareDir := filepath.Join(tmpDir, "share", "man")
	require.NoError(t, os.MkdirAll(shareDir, 0755))
	assert.True(t, r.hasLocalMapLayout(tmpDir))

	// Dir with bin containing auxiliary script
	tmpDir2 := t.TempDir()
	binDir := filepath.Join(tmpDir2, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "aux-helper"), []byte("#!/bin/sh"), 0755))

	rWithBinary := &GithubRelease{
		InstalledBinaries: []string{"main-app"},
	}
	assert.True(t, rWithBinary.hasLocalMapLayout(tmpDir2))

	// If the only binary in bin is the primary binary itself, no extra local map
	rPrimaryOnly := &GithubRelease{
		InstalledBinaries: []string{"aux-helper"},
	}
	assert.False(t, rPrimaryOnly.hasLocalMapLayout(tmpDir2))
}

func TestSymlinkSidecars_AutoMode(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	targetBin := filepath.Join(homeDir, ".local", "bin")
	require.NoError(t, os.MkdirAll(targetBin, 0755))

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	xdg.Reload()

	// 1. Package with Unix layout -> Auto chooses local-map
	pkgDir := filepath.Join(tmpDir, "pkg-with-layout")
	manDir := filepath.Join(pkgDir, "share", "man", "man1")
	libDir := filepath.Join(pkgDir, "lib")
	require.NoError(t, os.MkdirAll(manDir, 0755))
	require.NoError(t, os.MkdirAll(libDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(manDir, "tool.1"), []byte(".TH TOOL 1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "libtool.so"), []byte("binary data"), 0755))

	rAuto := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository: "coolorg/cooltool",
			CommonInstallFlags: params.CommonInstallFlags{
				SidecarFlags: params.SidecarFlags{
					IncludeSidecars: true,
					SidecarMode:     "auto",
				},
				TargetPath: targetBin,
			},
		},
	}

	err := rAuto.symlinkSidecars(pkgDir)
	require.NoError(t, err)

	// Verify mapped into ~/.local/share/man/man1 and ~/.local/lib
	expectedMan := filepath.Join(homeDir, ".local", "share", "man", "man1", "tool.1")
	expectedLib := filepath.Join(homeDir, ".local", "lib", "libtool.so")
	assert.FileExists(t, expectedMan)
	assert.FileExists(t, expectedLib)
	assert.Contains(t, rAuto.InstalledSidecars, expectedMan)
	assert.Contains(t, rAuto.InstalledSidecars, expectedLib)

	// 2. Flat package without Unix layout -> Auto falls back to xdg_data_home
	pkgFlat := filepath.Join(tmpDir, "pkg-flat")
	require.NoError(t, os.MkdirAll(pkgFlat, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgFlat, "config.json"), []byte(`{"key":"val"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgFlat, "tool.h"), []byte(`#define FOO 1`), 0644))

	rFlat := &GithubRelease{
		CliParams: &params.ExecContext{
			Repository: "flatorg/flattool",
			CommonInstallFlags: params.CommonInstallFlags{
				SidecarFlags: params.SidecarFlags{
					IncludeSidecars: true,
					SidecarMode:     "auto",
				},
				TargetPath: targetBin,
			},
		},
	}

	err = rFlat.symlinkSidecars(pkgFlat)
	require.NoError(t, err)

	expectedXDGJson := filepath.Join(xdg.DataHome, "gh-pt", "sidecars", "flatorg/flattool", "config.json")
	expectedXDGH := filepath.Join(xdg.DataHome, "gh-pt", "sidecars", "flatorg/flattool", "tool.h")
	assert.FileExists(t, expectedXDGJson)
	assert.FileExists(t, expectedXDGH)
}
