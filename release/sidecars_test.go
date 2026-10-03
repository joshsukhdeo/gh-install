package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockTTYPrompter struct {
	MockConfirm       bool
	MockInput         string
	MockMultiselect   []string
	MultiselectErr    error
	MultiselectCalled bool
	MultiselectPrompt string
	MultiselectOpts   []string
}

func (m *MockTTYPrompter) Confirm(message string) bool {
	return m.MockConfirm
}

func (m *MockTTYPrompter) Input(prompt string, defaultValue string) string {
	return m.MockInput
}

func (m *MockTTYPrompter) Multiselect(prompt string, options []string) ([]string, error) {
	m.MultiselectCalled = true
	m.MultiselectPrompt = prompt
	m.MultiselectOpts = options
	return m.MockMultiselect, m.MultiselectErr
}

func TestDetectSuspectedRemoteSidecars(t *testing.T) {
	// Should be flagged
	assert.True(t, isSuspectedRemoteSidecar("plugins.zip"), "plugins.zip should be flagged")
	assert.True(t, isSuspectedRemoteSidecar("game_data.pak"), "game_data.pak should be flagged")
	assert.True(t, isSuspectedRemoteSidecar("ai_model.bin"), "ai_model.bin should be flagged")
	assert.True(t, isSuspectedRemoteSidecar("extra_assets.tar.gz"), "extra_assets.tar.gz should be flagged")
	assert.True(t, isSuspectedRemoteSidecar("libcustom.so"), "libcustom.so should be flagged")
	assert.True(t, isSuspectedRemoteSidecar("render.red"), "render.red should be flagged")

	// Should be filtered out
	assert.False(t, isSuspectedRemoteSidecar("source.zip"), "source.zip should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("source.tar.gz"), "source.tar.gz should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app-src.zip"), "app-src.zip should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("checksums.txt"), "checksums.txt should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("sha256sums.txt"), "sha256sums.txt should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.sha256"), "app.sha256 should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.exe"), "app.exe should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("installer.msi"), "installer.msi should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.dmg"), "app.dmg should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.pkg"), "app.pkg should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.apk"), "app.apk should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.deb"), "app.deb should be filtered out")
	assert.False(t, isSuspectedRemoteSidecar("app.rpm"), "app.rpm should be filtered out")
}

func TestDetectSuspectedLocalSidecars(t *testing.T) {
	// Flagged
	assert.True(t, isSuspectedLocalSidecar("plugins/libcustom.so", "libcustom.so"))
	assert.True(t, isSuspectedLocalSidecar("libfoo.so.1.0", "libfoo.so.1.0"))
	assert.True(t, isSuspectedLocalSidecar("winplugin.dll", "winplugin.dll"))
	assert.True(t, isSuspectedLocalSidecar("libmac.dylib", "libmac.dylib"))
	assert.True(t, isSuspectedLocalSidecar("config/settings.json", "settings.json"))
	assert.True(t, isSuspectedLocalSidecar("config.yaml", "config.yaml"))
	assert.True(t, isSuspectedLocalSidecar("config.yml", "config.yml"))
	assert.True(t, isSuspectedLocalSidecar("theme.pak", "theme.pak"))
	assert.True(t, isSuspectedLocalSidecar("module.red", "module.red"))

	// Bloat filtered out
	assert.False(t, isSuspectedLocalSidecar("README.md", "README.md"))
	assert.False(t, isSuspectedLocalSidecar("readme.txt", "readme.txt"))
	assert.False(t, isSuspectedLocalSidecar("LICENSE", "LICENSE"))
	assert.False(t, isSuspectedLocalSidecar("LICENSE.txt", "LICENSE.txt"))
	assert.False(t, isSuspectedLocalSidecar("doc/manual.html", "manual.html"))
	assert.False(t, isSuspectedLocalSidecar("docs/guide.md", "guide.md"))
	assert.False(t, isSuspectedLocalSidecar("src/main.c", "main.c"))
	assert.False(t, isSuspectedLocalSidecar(".gitignore", ".gitignore"))
}

func TestSidecars_ConditionalRouting(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(targetPath, 0755))

	t.Run("Interactive mode with TTY presents Multiselect prompt", func(t *testing.T) {
		prompter := &MockTTYPrompter{
			MockConfirm:     true,
			MockMultiselect: []string{"plugins.zip"},
		}

		p := &params.ExecContext{
			CommonInstallFlags: params.CommonInstallFlags{
				Interactive:        true,
				DisablePrompts:     false,
				TargetPath:         targetPath,
				WarnUnmappedAssets: true,
			},
			Repository: "owner/repo",
		}

		gr := &GithubRelease{
			CliParams: p,
			Prompter:  prompter,
			IsTTYFunc: func() bool { return true },
		}

		suspected := []string{"plugins.zip", "data.pak"}
		selected, err := gr.handleSuspectedSidecars(suspected, "", nil, 123)
		require.NoError(t, err)
		assert.True(t, prompter.MultiselectCalled)
		assert.Equal(t, "Suspected sidecar assets detected. Select items to deploy:", prompter.MultiselectPrompt)
		assert.Equal(t, suspected, prompter.MultiselectOpts)
		assert.Equal(t, []string{"plugins.zip"}, selected)
		// The function returns the selected items
		// InstalledSidecars is populated when files are actually copied/downloaded
	})

	t.Run("Headless mode with DisablePrompts does not prompt", func(t *testing.T) {
		prompter := &MockTTYPrompter{
			MockConfirm: true,
		}

		p := &params.ExecContext{
			CommonInstallFlags: params.CommonInstallFlags{
				Interactive:    true,
				DisablePrompts: true,
				TargetPath:     targetPath,
			},
			Repository: "owner/repo",
		}

		gr := &GithubRelease{
			CliParams: p,
			Prompter:  prompter,
			IsTTYFunc: func() bool { return true },
		}

		suspected := []string{"plugins.zip"}
		selected, err := gr.handleSuspectedSidecars(suspected, "", nil, 123)
		require.NoError(t, err)
		assert.False(t, prompter.MultiselectCalled, "should not prompt in headless mode")
		assert.Empty(t, selected)
		assert.Empty(t, gr.Sidecars)
	})

	t.Run("Headless mode with Non-TTY does not prompt", func(t *testing.T) {
		prompter := &MockTTYPrompter{
			MockConfirm: true,
		}

		p := &params.ExecContext{
			CommonInstallFlags: params.CommonInstallFlags{
				Interactive:    true,
				DisablePrompts: false,
				TargetPath:     targetPath,
			},
			Repository: "owner/repo",
		}

		gr := &GithubRelease{
			CliParams: p,
			Prompter:  prompter,
			IsTTYFunc: func() bool { return false },
		}

		suspected := []string{"plugins.zip"}
		selected, err := gr.handleSuspectedSidecars(suspected, "", nil, 123)
		require.NoError(t, err)
		assert.False(t, prompter.MultiselectCalled, "should not prompt when non-TTY")
		assert.Empty(t, selected)
		assert.Empty(t, gr.Sidecars)
	})

	t.Run("WarnUnmappedAssets false suppresses prompts and warnings", func(t *testing.T) {
		prompter := &MockTTYPrompter{
			MockConfirm: true,
		}

		warnFalse := false
		p := &params.ExecContext{
			CommonInstallFlags: params.CommonInstallFlags{
				Interactive:    true,
				DisablePrompts: false,
				TargetPath:     targetPath,
			},
			Repository: "owner/repo",
		}

		gr := &GithubRelease{
			CliParams:          p,
			Prompter:           prompter,
			IsTTYFunc:          func() bool { return true },
			WarnUnmappedAssets: &warnFalse,
		}

		suspected := []string{"plugins.zip"}
		selected, err := gr.handleSuspectedSidecars(suspected, "", nil, 123)
		require.NoError(t, err)
		assert.False(t, prompter.MultiselectCalled)
		assert.Empty(t, selected)
	})
}

func TestSidecars_AlwaysExcludeLicense(t *testing.T) {
	// 1. isLicenseFileName helper assertions
	assert.True(t, isLicenseFileName("LICENSE"))
	assert.True(t, isLicenseFileName("license"))
	assert.True(t, isLicenseFileName("LICENSE.txt"))
	assert.True(t, isLicenseFileName("LICENSE.md"))
	assert.True(t, isLicenseFileName("LICENSE-MIT"))
	assert.True(t, isLicenseFileName("LICENSE-APACHE"))
	assert.True(t, isLicenseFileName("UNLICENSE"))
	assert.True(t, isLicenseFileName("COPYING"))
	assert.True(t, isLicenseFileName("/some/dir/LICENSE"))
	assert.True(t, isLicenseFileName("LICENCE"))
	assert.False(t, isLicenseFileName("mediamtx.yml"))
	assert.False(t, isLicenseFileName("config.json"))
	assert.False(t, isLicenseFileName("libplugin.so"))

	// 2. Default sidecar regexes must NOT match LICENSE
	gr := &GithubRelease{}
	defaultRegex := gr.getDefaultSidecarRegex("/usr/local/bin")
	assert.NotContains(t, defaultRegex, "LICENSE")
	xdgRegex := gr.getDefaultSidecarRegex("/home/user/.local/share")
	assert.NotContains(t, xdgRegex, "LICENSE")
	customRegex := gr.getDefaultSidecarRegex("/opt/custom")
	assert.NotContains(t, customRegex, "LICENSE")

	// 3. extractExplicitSidecars with wildcard regex must exclude LICENSE
	tmpDir := t.TempDir()
	extractDir := filepath.Join(tmpDir, "extract")
	targetDir := filepath.Join(tmpDir, "target")
	require.NoError(t, os.MkdirAll(extractDir, 0755))
	require.NoError(t, os.MkdirAll(targetDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "LICENSE"), []byte("license"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "LICENSE.txt"), []byte("license"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "plugin.so"), []byte("code"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "config.yml"), []byte("key: val"), 0644))

	gr = &GithubRelease{
		CliParams: &params.ExecContext{
			CommonInstallFlags: params.CommonInstallFlags{
				TargetPath: targetDir,
				Sidecars:   ".*", // Wildcard matching everything
			},
			Repository: "owner/repo",
		},
	}

	err := gr.extractExplicitSidecars(extractDir, nil)
	require.NoError(t, err)

	for _, sc := range gr.InstalledSidecars {
		assert.NotContains(t, strings.ToLower(filepath.Base(sc)), "license", "LICENSE must never be in InstalledSidecars")
	}
	assert.Len(t, gr.InstalledSidecars, 2, "Only plugin.so and config.yml should be installed")
}
